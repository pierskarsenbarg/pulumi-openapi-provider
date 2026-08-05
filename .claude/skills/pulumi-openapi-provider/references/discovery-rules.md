# Discovery rules

How `pulumi-openapi-provider` turns spec paths into Pulumi resources. Written from
`pkg/spec/resource.go` (grouping, CRUD detection, types, auth extraction),
`pkg/spec/schema.go` (schema emission) and `pkg/parameterized/parameterized.go`
(package name/version), and checked by running discovery on fixtures.

Contents:
1. [Grouping paths into resources](#1-grouping-paths-into-resources)
2. [Detecting CRUD operations](#2-detecting-crud-operations)
3. [Resource names, tokens and modules](#3-resource-names-tokens-and-modules)
4. [Context path params](#4-context-path-params)
5. [Inputs, outputs and the ID](#5-inputs-outputs-and-the-id)
6. [Type mapping](#6-type-mapping)
7. [Enums](#7-enums)
8. [Base URL](#8-base-url)
9. [Package name and version (parameterized mode)](#9-package-name-and-version-parameterized-mode)
10. [Worked example: Swagger Petstore (OAS2)](#10-worked-example-swagger-petstore-oas2)
11. [Worked example: nested org/team API (OAS3)](#11-worked-example-nested-orgteam-api-oas3)
12. [Known documentation drift](#12-known-documentation-drift)

Swagger 2.0 and OAS3 go down separate code paths (`discoverV2` / `discoverV3`,
selected on `SpecFormat == "oas2"`), but they share `groupPathStrings` and behave
identically except where noted. Both accept `oas3`, `oas3_1` and `oas3_2`.

---

## 1. Grouping paths into resources

Only the path *strings* matter here — methods are not consulted yet.

```
sort paths deepest-first (by number of "/")
for each path P:
    if P's last segment is not "{param}"      -> skip P (not an item path)
    if P was already claimed as a collection  -> skip P
    idParam        = last segment without braces
    collectionPath = P minus its last segment
    name           = PascalCase join of collectionPath's static segments
    emit group(name, collectionPath, P, idParam)
    mark collectionPath as claimed
```

Testable predicates for a candidate path `P`:

- **Is `P` an item path?** Its last segment is `{...}`. Only item paths seed groups.
- **Does `P` become a collection instead?** Yes if a deeper item path's parent is exactly `P`,
  because deepest-first ordering lets the deeper group claim it. Usually this doesn't bite:
  `/orgs/{orgId}/teams/{teamId}`'s parent is `/orgs/{orgId}/teams`, so `/orgs/{orgId}`
  survives and both `Orgs` and `OrgsTeams` are discovered. It does bite with directly nested
  params — `/orgs/{orgId}` + `/orgs/{orgId}/{teamId}` yields one group (named `Orgs`, but
  managing teams), and the org resource vanishes.
- **Trailing slashes**: preserved on the emitted paths (`/widgets/` → collection `/widgets/`,
  item `/widgets/{widgetId}/`) and normalised for the claimed-collection check. Specs where
  every path ends in `/` (NetBox, DRF-generated specs) work.
- **Duplicate item paths** are deduplicated; the first (deepest) wins.

## 2. Detecting CRUD operations

Given a group, operations are read only from the two paths in that group. Both the
exact path and the path plus `/` are looked up, so trailing-slash specs match.

| Operation | Looked for at | Notes |
| --------- | ------------- | ----- |
| Create | `POST` on the **collection** path | Method is always POST; `CreateMethod` is set to `"POST"` |
| Read | `GET` on the **item** path | |
| Update | `PUT` on the item path, else `PATCH` on the item path | PUT wins when both exist |
| Delete | `DELETE` on the item path | |

**Viability rule** — the group is discarded silently unless:

```
createOp != nil  AND  (readOp != nil OR deleteOp != nil)
```

So `GET /things/{id}` with no `POST /things` yields nothing at all, and a create-only
group yields nothing either.

**Update is optional.** A resource with no update is still emitted, with
`UpdatePath == ""`; at runtime `update` returns the inputs unchanged (no HTTP call),
so property changes appear to apply but never reach the API unless Pulumi decides to
replace. Endpoints that discovery will *not* treat as updates:

- `PUT`/`PATCH` on the collection path (update by ID in the request body)
- `POST` on the item path (the Swagger Petstore's form-encoded pet update)
- any update under a different path (`/things/{id}/rename`)

All three need `ResourceOverride{UpdatePath, UpdateMethod}` (library mode). The bundled
preflight harness flags the first two as `CANDIDATE` lines.

**Tags** never affect *whether* a group is discovered — only which module its token lands
in, and `ExcludeTags`, which drops the whole group if any of its operations carries an
excluded tag.

## 3. Resource names, tokens and modules

Name = PascalCase join of the collection path's **static** segments; `{param}` segments
are skipped, and `-`, `_`, `.` split words.

| Collection path | Name |
| --------------- | ---- |
| `/pet` | `Pet` |
| `/store/order` | `StoreOrder` |
| `/orgs/{orgId}/teams` | `OrgsTeams` |
| `/api/widgets` | `Widgets` (a leading `api` segment is stripped) |
| `/extras/gadgets` | `ExtrasGadgets` (only `api` is special) |
| `/v2/ip-addresses` | `V2IpAddresses` |

A group whose name comes out empty is dropped.

Token = `<pkgName>:<module>:<Name>`. The **module** is the first tag on any of the
group's operations that is also declared in the spec's root `tags` list, lowercased and
stripped of non-alphanumerics (`"AI Content"` → `aicontent`); otherwise `index`. This
means a spec with root tags produces `petstore:pet:Pet`, not `petstore:index:Pet` —
which changes SDK import paths, so check the harness output rather than assuming
`index`. Complex/enum types always live in `<pkgName>:index:<Type>`.

## 4. Context path params

Every `{param}` in the item path other than the trailing ID param is a "context param"
(`{orgId}` in `/orgs/{orgId}/teams/{teamId}`). Each one is added as a **required string
input** (and output) if the body schema doesn't already define it, and is substituted
into the URL at call time from the inputs/state map.

Consequence for users: nested resources take the parent ID as an ordinary input, e.g.
`new Team("t", { orgId: org.id, name: "core" })`.

## 5. Inputs, outputs and the ID

- **Inputs** come from the create operation's body schema: Swagger 2.0 uses the `body`
  parameter's schema; OAS3 uses `requestBody.content["application/json"].schema` (if that
  resolves to a property-less `oneOf`, the first non-array variant is used — NetBox-style
  bulk wrappers). No JSON request body means no inputs beyond context params.
- **Outputs** = inputs plus any extra properties from the read operation's first
  `200`/`201`/`202` JSON response schema (falling back to the create operation's response
  when there is no read).
- **Property names are camelCased** (`display_name` → `displayName`) and the original API
  names are remembered, so request bodies and responses are translated both ways.
- **Required inputs** come from the body schema's `required` list, camelCased, minus the ID.
- **`id` is reserved by Pulumi** and is always removed from inputs and outputs.

**ID resolution** — this is the highest-risk part of a build:

```
IDPathParam = the group's trailing path param        (e.g. "orgId")
IDField     = "id" if the read/create response schema declares an "id" property,
              otherwise IDPathParam
```

At create time the raw JSON response is searched for `IDField`, then `IDPathParam`, then
`"id"` (dot notation traverses nested objects, e.g. `metadata.name`). If none is present,
create fails with:

```
create <Name>: could not extract ID from response (looked for field "<IDField>")
```

The lookup uses **API names, not camelCase**, so a response of `{"org_id": "..."}` with
`IDField == "orgId"` fails — verified against the runtime. Fix with
`ResourceOverride{IDField: "org_id"}`. A `201` with an empty body or only a `Location`
header fails the same way and needs a `Create` hook.

## 6. Type mapping

| OpenAPI | Pulumi |
| ------- | ------ |
| `string` | `string` |
| `integer` | `integer` |
| `number` | `number` |
| `boolean` | `boolean` |
| `array` | `array` with item type |
| `object` | `object` (untyped bag — inline object properties are **not** expanded) |
| `$ref` to a definition/component | `$ref` to `#/types/<pkg>:index:<PascalName>` |
| anything else, or no `type` | `string` |

Details worth knowing before promising fidelity:

- Only inline `type: object` is flattened to an opaque `object`; a `$ref`'d object becomes a
  proper named type with camelCased properties and required list.
- Array items that are `$ref`s become type refs; **arrays of arrays fall back to `string`**
  because the inner item type can't be represented.
- `format` (`date-time`, `int64`, `uuid`) is ignored — everything is the base type.
- `allOf`/`anyOf` composition is not merged; only the `oneOf` request-body case above is
  handled. A schema built purely from `allOf` yields a property-less type.
- Reference cycles are safe (a placeholder type is registered before recursing).

## 7. Enums

Two registration paths, both producing real Pulumi enum types:

- **Named enum** — a definition/component whose schema has `enum` values is registered as
  `<pkg>:index:<PascalName>` with `type` set to the Pulumi equivalent of its OpenAPI type.
- **Inline enum** — a property with `enum` values becomes a named type derived from its
  context: `<ResourceName><PascalPropertyName>` for resource-level properties
  (`Pet.status` → `PetStatus`), `<TypeName><PascalPropertyName>` for nested ones.
  The property then `$ref`s that type instead of being a plain string.

Value types are preserved from the YAML tag (`!!int`, `!!float`, `!!bool`, else string).

**Dropped values**: `""` and `null` are skipped, because an empty string produces an
unnamed Go constant that collides with the type name. Verified: `["", "public", "private"]`
emits `public | private`, and `["small", "large", null]` emits `small | large`. Nothing warns
about this — call it out in preflight, and if the dropped value is meaningful (many APIs use
`""` for "unset"), the property should not be an enum at all.

## 8. Base URL

| Spec | Source |
| ---- | ------ |
| OAS3 | `servers[0].url` verbatim (later servers ignored) |
| Swagger 2.0 | `<scheme>://<host><basePath>`, preferring `https` over `http`; `basePath: "/"` is dropped |

No host/servers → empty string, and **nothing fails until the first API call**, which
errors with `baseUrl is not set: provide it via provider config or ensure the spec declares
a server URL`. Precedence: `Options.BaseURL` (library) or `--base-url` (parameterized)
beats the spec, and the `baseUrl` provider config setting beats both at runtime.

## 9. Package name and version (parameterized mode)

`pulumi package add openapi-provider <spec>` derives both from `info`:

- **Name** = `info.title` lowercased with every non-alphanumeric run collapsed to a single
  `-`, trimmed (`"Petstore API"` → `petstore-api`, `"Trap API!! v2"` → `trap-api-v2`).
  Empty title → `openapi`.
- **Version** = `info.version` split on `.` only, each part truncated at its first
  non-digit, padded to three components (`"1.0.7"` → `1.0.7`, `"v1.2"` → `1.2.0`,
  `"1.2.3-beta"` → `1.2.3`, `"2024-05-01"` → `2024.0.0` — hyphens are not separators,
  `"beta"` → `1.0.0`).

The name is the config namespace and SDK package name, so report it explicitly.

The schema also gets a `parameterization` block whose `parameter` blob is
`{"spec": "<url-or-path>", "baseURL": "<--base-url if given>"}` — embedded in generated
SDKs and echoed back on re-parameterization. An empty `baseURL` means "re-derive from the
spec next time", so a spec that later gains a `servers` entry picks it up.

## 10. Worked example: Swagger Petstore (OAS2)

Paths: `/pet` (POST, PUT), `/pet/findByStatus` (GET), `/pet/{petId}` (GET, POST, DELETE),
`/pet/{petId}/uploadImage` (POST), `/store/inventory` (GET), `/store/order` (POST),
`/store/order/{orderId}` (GET, DELETE), `/user` (POST), `/user/createWithList` (POST),
`/user/login` (GET), `/user/{username}` (GET, PUT, DELETE).

Predict:

- Item paths are `/pet/{petId}`, `/store/order/{orderId}`, `/user/{username}` →
  three groups: `Pet`, `StoreOrder`, `User`. `/pet/{petId}/uploadImage` ends in a static
  segment, so it seeds nothing and doesn't claim `/pet/{petId}` as a collection.
- **Pet**: create `POST /pet`, read/delete on the item path, **no update** — `PUT /pet` is on
  the collection and `POST /pet/{petId}` is on the item path, and neither is where discovery
  looks. Root tags `pet`/`store`/`user` exist, so the token is `petstore:pet:Pet`.
- **StoreOrder**: create `POST /store/order`, read/delete, no update.
- **User**: create `POST /user`, read/update(`PUT`)/delete on `/user/{username}`.
- IDs: the `Pet`, `Order` and `User` definitions all declare `id`, so `IDField` is `"id"`
  for all three even though the path params are `petId`/`orderId`/`username`. `id` is
  stripped from inputs and outputs.
- Enums: `Pet.status` → `petstore:index:PetStatus` (`available | pending | sold`);
  `Order.status` → `petstore:index:StoreOrderStatus`.
- Auth: `api_key` (apiKey in header) → config var `api_key`; `petstore_auth` (oauth2) →
  config var `bearerToken` sending `Authorization: bearer <token>`. Plus `baseUrl`.
- Unmapped: `/pet/findByStatus`, `/pet/{petId}/uploadImage`, `/store/inventory`,
  `/user/createWithList`, `/user/login` — list/search/action endpoints, all expected.

So the honest summary for a user asking for "a Petstore provider" is: three resources,
`User` is the only one that can be updated in place, and `POST /user` returns no `id`
field, so creating a `User` will fail ID extraction unless a `Create` hook or `IDField`
override handles it.

## 11. Worked example: nested org/team API (OAS3)

Paths: `/orgs` (POST), `/orgs/{orgId}` (GET, DELETE), `/orgs/{orgId}/teams` (POST),
`/orgs/{orgId}/teams/{teamId}` (GET, PATCH, DELETE), no `servers`, `Org` has `org_id`,
`Team` has `id`.

Predict:

- Deepest item path first: `/orgs/{orgId}/teams/{teamId}` → collection
  `/orgs/{orgId}/teams`, name `OrgsTeams`, ID param `teamId`. `/orgs/{orgId}` is not that
  group's parent, so it still seeds its own group `Orgs`.
- `OrgsTeams` has update `PATCH /orgs/{orgId}/teams/{teamId}`; `{orgId}` is a context param
  → required string input `orgId` alongside `name`.
- `Orgs`: `IDField` = `orgId` (no `id` property on `Org`) — and because `org_id` camelCases
  to `orgId`, it is stripped from the inputs. At create, the response `{"org_id": "..."}`
  is searched for `orgId` and fails. Fix: `ResourceOverride{"Orgs": {IDField: "org_id"}}`.
- No `servers` → `baseUrl` empty, so `--base-url` / `Options.BaseURL` is mandatory.

## 12. Known documentation drift

The README and CLAUDE.md are behind the code in these places — trust the code:

- **Tokens**: both suggest `<pkg>:index:<Name>`; modules are actually tag-derived when the
  spec declares root tags.
- **Parameterization blob**: CLAUDE.md says `{"specURL": …}`; the JSON key is `spec`.
- **Base URL**: the README says `pulumi package add` "exits with a clear error" when no base
  URL is available; it succeeds, and the failure surfaces on the first API call.
- **Bearer prefix**: the README shows `Authorization: Bearer <value>`; the default prefix is
  lowercase `bearer`.
- **Undocumented options**: `ExcludeTags`, `PollingOptions`/`DisablePolling`, `UserAgent`,
  and the `Check`/`Diff`/`Create`/`Read`/`Update`/`Delete` function hooks on
  `ResourceOverride` are all real and absent from the README's table.
- **Enum drops**: the README mentions empty strings; `null` values are dropped too.
