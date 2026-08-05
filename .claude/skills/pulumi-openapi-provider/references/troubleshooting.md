# Troubleshooting

Symptom → cause → fix. Error strings are quoted from the code so they can be matched
against what the user pasted.

Reminder before diagnosing: run the preflight harness (`scripts/preflight`) against the
spec. Most of these are visible in its output in seconds, and it needs no credentials.

---

## A resource is missing from the generated SDK

Discovery drops groups silently, so "missing" is normal output, not a bug. In order of
likelihood:

1. **No `POST` on the collection path.** Create is mandatory. `GET /things/{id}` alone
   yields nothing. If creation happens through an RPC-ish endpoint (`POST /things/create`),
   use `ResourceOverride{CreatePath: "/things/create"}` on a group that *is* discovered, or
   write the resource by hand with `WithResources`.
2. **Neither read nor delete on the item path.** A group needs create plus one of them.
3. **The path doesn't end in `{param}`.** `/things/{id}/detail` and `/things/latest` never
   seed a group.
4. **The parent was claimed by a deeper group** (directly nested params, `/orgs/{orgId}` +
   `/orgs/{orgId}/{teamId}`). Check the harness's resource list for a group with the parent's
   name but the child's paths.
5. **`ExcludeTags` matched one of its operations**, or an override keyed to its name has
   `Skip: true`.
6. **The name you're looking for isn't the name it got.** `/api/v2/ip-addresses` becomes
   `V2IpAddresses`; check the harness output before concluding it's missing.

## `create <Name>: could not extract ID from response (looked for field "X")`

The create response had no usable ID. The lookup tries `IDField`, then `IDPathParam`, then
`"id"`, against the **raw API JSON** (original names, not camelCase), with dot notation for
nesting.

- Response uses `org_id` but `IDField` is `orgId` → `ResourceOverride{IDField: "org_id"}`.
- ID is nested → `IDField: "metadata.name"` (see `examples/openapi-k8s`).
- Create returns 201 with an empty body or only a `Location` header → no field to read;
  supply a `Create` hook that issues the POST and derives the ID itself.
- Create returns a wrapper (`{"data": {"id": …}}`) → `IDField: "data.id"`.

Everything here is library mode; the parameterized binary can't express any of it.

## `baseUrl is not set: provide it via provider config or ensure the spec declares a server URL`

The spec had no `servers[0].url` / `host`, and nothing supplied one. Note this appears on the
first API call, not at package-add or provider-start time — the README's claim that
`pulumi package add` fails early is wrong.

- Parameterized: `pulumi package add openapi-provider <spec> --base-url=https://api.example.com`
  (re-run it; the base URL is baked into the parameterization blob), or
  `pulumi config set <pkg>:baseUrl https://api.example.com`.
- Library: `Options.BaseURL`, or tell users to set `<pkg>:baseUrl`.

## Every call returns 401/403

1. **The credential config variable isn't the name they expect.** apiKey variables are named
   after the *scheme key* with only the first letter lowercased — `X-Auth-Token` becomes
   `x-Auth-Token`. Print the exact `pulumi config set` line from the harness output.
2. **`apiKey in: query`** — discovered, surfaced as config, and never sent. This is a known
   gap with no override. Workaround: library mode with a custom `HTTPClient` whose
   `RoundTripper` appends the query parameter.
3. **Prefix mismatch.** The default is lowercase `bearer <token>`. APIs wanting `token <x>`,
   `Bearer <x>` exactly, or a raw token need `AuthOverride{TokenPrefix: …}` (library mode).
4. **Wrong header.** A spec that declares `apiKey in: header, name: Authorization` is treated
   as bearer; if the API really wants a different header, `AuthOverride{HeaderName: …}`.
5. **The value never reached the provider.** Config is read in `Configure` by exact variable
   name; a typo'd key is ignored silently. `pulumi config` to confirm.

## An enum constant is missing from the SDK

`""` and `null` enum values are dropped (an empty string yields an unnamed constant that
collides with the type name). Nothing warns. If the dropped value is semantically meaningful
— many APIs use `""` for "unset" — the property shouldn't be an enum: fix the spec, or accept
that the value can't be expressed and document it for users.

## Changing a property does nothing / Pulumi replaces instead of updating

The resource has no update endpoint (`UpdatePath == ""`), so `update` returns the inputs
unchanged without calling the API — and because the built-in diff never asks for a
replacement, `pulumi up` reports a successful update that never reached the API. Discovery
only accepts `PUT`/`PATCH` on the item path; `PUT /things` (body ID) and `POST /things/{id}`
are ignored. The preflight harness prints `CANDIDATE` lines for exactly these cases.

Fixes, in order of preference:

- The endpoint takes the ID in the URL → `ResourceOverride{UpdatePath, UpdateMethod}`.
- The endpoint takes the ID in the body → an `Update` hook. A path override is not enough:
  `id` is stripped from the input schema during discovery, so the generated body would carry
  no ID, and APIs that treat a PUT without an ID as a create will silently duplicate the
  resource.
- Updates aren't expressible at all → a `Diff` hook that forces replacement, so Pulumi
  routes changes through delete/create instead of reporting a no-op update.

## `pulumi up` shows a diff on every run

The built-in diff compares stringified property values and marks anything in state but not in
inputs as deleted. Server-computed outputs (timestamps, computed status) that arrive in the
create/read response will therefore churn. Options: a `Diff` hook that ignores those
properties, or `DisablePolling: true` if the churn comes from the post-create re-read pulling
in fields the API mutates.

## Resource names are ugly or collide

Names are PascalCase joins of static collection-path segments, with only a leading `api`
stripped. `ResourceOverride{Token: "pkg:index:Nicer"}` renames one; there is no bulk rename.
Note the module segment in a token is tag-derived, so keep it consistent with what the
harness reports if you only mean to change the type name.

## Token module isn't `index` and imports look wrong

Expected: the module comes from the first operation tag that also appears in the spec's root
`tags` list (`petstore:pet:Pet`). The README and CLAUDE.md say `index`; the code disagrees.
Types and enums are always in `index`. Override with `ResourceOverride{Token}` if a flat
namespace is wanted.

## Nested object properties are untyped

A property declared as an inline `type: object` maps to an opaque Pulumi `object` — only
`$ref`'d schemas become named types with real fields. Same for arrays of arrays, which
degrade to `string`. `allOf`/`anyOf` are not merged, so a schema composed purely of `allOf`
can produce a type with no properties. Fixing this means fixing the spec (hoist the inline
object into `components/schemas` and `$ref` it).

## The spec won't load

- `not a recognised OpenAPI/Swagger spec (spec format "…")` — the document parsed but wasn't
  OAS2/OAS3.x. Check for a wrapper (some vendors serve `{"spec": {...}}`).
- Fetch failures over HTTP: the loader sends `User-Agent: pulumi-openapi-provider/<version>`;
  some CDNs block unknown agents. Set `Options.UserAgent`, or download the spec and use
  `SpecPath` / a local file with `pulumi package add openapi-provider ./openapi.json`.
- Non-2xx responses aren't detected on spec fetch — the body is parsed anyway, so an HTML
  error page surfaces as a parse error rather than a 404.

## Nothing happens after `pulumi package add`

If the generated SDK has no resources, discovery found no viable group. That's an RPC-shaped
or read-only API — see "When this library is the wrong tool" in SKILL.md. Confirm with the
harness before telling the user; "zero resources" is worth stating plainly along with which
endpoints would need to change shape.
