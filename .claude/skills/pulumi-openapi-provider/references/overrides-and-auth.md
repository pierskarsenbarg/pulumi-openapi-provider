# Overrides, hooks and auth

Everything here is **library mode only**. The parameterized binary calls
`spec.Discover(doc, pkgName, nil, nil)` and builds its config with no overrides, so
`Overrides`, `ExcludeTags`, `AuthOverride`, `BaseURL`, `HTTPClient` and polling options
have no effect there. The moment preflight says one of these is needed, the answer is a
Go `main.go`.

Source: `types.go` (public options), `provider.go` (wiring), `pkg/spec/resource.go`
(override application), `pkg/config/config.go` (auth at runtime).

Contents:
1. [Options reference](#1-options-reference)
2. [ResourceOverride: declarative fields](#2-resourceoverride-declarative-fields)
3. [ResourceOverride: function hooks](#3-resourceoverride-function-hooks)
4. [The wildcard key](#4-the-wildcard-key)
5. [Auth: what the spec produces](#5-auth-what-the-spec-produces)
6. [Auth: what gets sent](#6-auth-what-gets-sent)
7. [AuthOverride](#7-authoverride)
8. [Polling](#8-polling)

---

## 1. Options reference

| Field | Purpose |
| ----- | ------- |
| `SpecURL` / `SpecPath` | Where to read the spec. One is required. |
| `BaseURL` | Overrides the spec's server URL at build time. Users can still override with `pulumi config set <pkg>:baseUrl`. |
| `HTTPClient` | Custom `*http.Client` for both the spec fetch and API calls — the hook for mTLS, kubeconfig transports, signing round-trippers, proxies. |
| `UserAgent` | Replaces the default `pulumi-openapi-provider/<version>` header. |
| `Overrides` | `map[resourceName]ResourceOverride`. Keys are discovered names (`"Pet"`, `"StoreOrder"`, `"OrgsTeams"`), not tokens. |
| `ExcludeTags` | Drops any resource whose create/read/update/delete operations carry one of these tags. The lever for trimming huge specs. |
| `AuthOverride` | Changes the header name and/or token prefix for bearer-style credentials. |
| `DisablePolling` | Turns off the post-create "wait until it exists" and post-delete "wait until it's gone" reads. |
| `PollingOptions` | Timeout / initial interval / max interval / multiplier for that polling. |

Override keys must match the **discovered** name exactly. If a key never matches, it is
silently ignored — a common cause of "my override did nothing". Confirm names with the
preflight harness first.

## 2. ResourceOverride: declarative fields

Applied after discovery; empty fields keep the convention-derived value.

| Field | Effect |
| ----- | ------ |
| `Skip` | Drop the resource entirely (checked before the resource is even built) |
| `Token` | Replace the Pulumi token, i.e. rename or re-module the resource |
| `CreatePath` / `CreateMethod` | Point create elsewhere / use a method other than POST |
| `ReadPath` | Point read elsewhere |
| `UpdatePath` / `UpdateMethod` | Add or move update — the fix for "no update endpoint" |
| `DeletePath` | Point delete elsewhere |
| `IDPathParam` | Change which `{param}` carries the resource ID |
| `IDField` | Change which **API-named** response field the ID is read from |

One example per reason:

**Update lives on the collection (body-ID update).** Discovery only looks for `PUT`/`PATCH`
on the item path, so `PUT /widgets` is invisible. Point update at the item path if the API
also accepts it there, or at the collection path if the ID travels in the body (the ID is
substituted into `{...}` placeholders, and inputs are sent as the JSON body either way):

```go
Overrides: map[string]openapi.ResourceOverride{
    "Widgets": {UpdatePath: "/widgets/{widgetId}", UpdateMethod: "PUT"},
}
```

**Create response uses a different ID key.** `{"org_id": "..."}` with an `IDField` of
`orgId` fails at create. Use the API's own name:

```go
"Orgs": {IDField: "org_id"},
```

**Nested ID.** Dot notation traverses the response (this is what `examples/openapi-k8s` does
for every resource):

```go
"*": {IDField: "metadata.name"},
```

**Rename a resource.** Path-derived names can be ugly (`ApiV2IpAddresses`):

```go
"ApiV2IpAddresses": {Token: "netbox:index:IpAddress"},
```

**Exclude a resource.** For internal/admin endpoints that happen to be CRUD-shaped:

```go
"InternalDebug": {Skip: true},
```

**Read lives elsewhere.** Some APIs expose the item read under a different prefix:

```go
"Jobs": {ReadPath: "/v1/jobs/{jobId}/status"},
```

## 3. ResourceOverride: function hooks

The same struct carries optional functions that replace the built-in behaviour entirely for
one resource. Any nil hook falls back to the generated implementation, so hooks are for the
one operation an API does strangely — not an all-or-nothing takeover.

```go
Check  func(ctx context.Context, req p.CheckRequest)  (p.CheckResponse, error)
Diff   func(ctx context.Context, req p.DiffRequest)   (p.DiffResponse, error)
Create func(ctx context.Context, req p.CreateRequest) (p.CreateResponse, error)
Read   func(ctx context.Context, req p.ReadRequest)   (p.ReadResponse, error)
Update func(ctx context.Context, req p.UpdateRequest) (p.UpdateResponse, error)
Delete func(ctx context.Context, req p.DeleteRequest) error
```

(`p` is `github.com/pulumi/pulumi-go-provider`.)

Reach for a hook when a path/field override can't express the behaviour:

- **Create returns no usable ID** (204, or the ID only in a `Location` header) → `Create`
- **Extra input validation** → `Check`, as in `integration-tests/code-provider/provider/main.go`:

  ```go
  "Users": {Check: func(ctx context.Context, req p.CheckRequest) (p.CheckResponse, error) {
      email, ok := req.Inputs.GetOk("email")
      if ok && email.IsString() && !strings.Contains(email.AsString(), "@") {
          return p.CheckResponse{Inputs: req.Inputs, Failures: []p.CheckFailure{
              {Property: "email", Reason: "email must contain @"}}}, nil
      }
      return p.CheckResponse{Inputs: req.Inputs}, nil
  }},
  ```

- **Server-computed fields cause permanent diffs** → `Diff` (the built-in diff is a
  stringified property comparison, and any output the API adds that isn't an input shows as
  a change).

Note the asymmetry: `Skip`/paths/IDs are consumed during discovery, hooks are wired into the
dispatch table by token afterwards. Both live in the same map entry.

## 4. The wildcard key

`Overrides["*"]` applies to every resource as a baseline; a named entry wins field by field
(and hook by hook), so you can set a global `IDField` and still special-case one resource.
`Skip` is *not* read from the wildcard — `"*": {Skip: true}` disables nothing.

```go
Overrides: map[string]openapi.ResourceOverride{
    "*":       {IDField: "metadata.name"},
    "Configs": {IDField: "uid"},
}
```

## 5. Auth: what the spec produces

Discovery reads `securityDefinitions` (Swagger 2.0) or `components.securitySchemes` (OAS3)
and turns each into a config variable. `baseUrl` is always present.

| Spec declaration | Kind | Config variable | Runtime effect |
| ---------------- | ---- | --------------- | -------------- |
| `apiKey` in header, `name: Authorization` | bearer | **scheme key**, first letter lowercased | `Authorization: bearer <value>` |
| `apiKey` in header, any other name | apiKey | **scheme key**, first letter lowercased | `<name>: <value>` |
| `apiKey` in query | apiKey | scheme key, first letter lowercased | **nothing is sent** |
| `http` scheme `bearer` (OAS3) | bearer | `bearerToken` | `Authorization: bearer <value>` |
| `http` scheme `basic` (OAS3) / `basic` (OAS2) | basic | `username` + `password` | `Authorization: Basic <base64>` |
| `oauth2`, `openIdConnect` | bearer | `bearerToken` | `Authorization: bearer <value>` |
| *(none declared)* | — | `apiKey`, `apiKeyHeader`, `bearerToken` | `apiKey` sent on `apiKeyHeader` (default `api_key`); `bearerToken` on `Authorization` |

Two naming traps:

- The apiKey config variable comes from the **scheme key in the spec**, not the header name,
  and only its first character is lowercased. A scheme keyed `X-Auth-Token` produces the
  config variable `x-Auth-Token` — so the user runs
  `pulumi config set myprovider:x-Auth-Token <value> --secret`. Verified; report the exact
  key from preflight rather than guessing.
- An `apiKey` scheme whose header *is* `Authorization` is reclassified as bearer: it keeps
  its scheme-key config variable (a scheme keyed `MyToken` stays `myToken`) but the value is
  now sent as `Authorization: bearer <value>` rather than verbatim — wrong for APIs expecting
  the raw token. That's what `AuthOverride{TokenPrefix: ""}` is for. Only `http` `bearer`,
  `oauth2` and `openIdConnect` schemes produce the variable literally named `bearerToken`.

All credential variables are marked secret. Multiple schemes coexist: every configured
credential is sent on every request, so a spec declaring both `api_key` and `oauth2`
produces two variables and sends whichever the user set.

## 6. Auth: what gets sent

`AuthHeaders()` builds the header map per request. The gap worth knowing: **`apiKey in:
query` is discovered and surfaced as config, but never applied** — the code notes that query
support needs changes in `crud.go`. Requests go out unauthenticated and the API answers 401
with nothing in the provider output pointing at the cause. There is no override for it; the
workarounds are a custom `HTTPClient` whose `RoundTripper` appends the query parameter, or
asking the API owner for a header-based key.

Requests also always carry `Content-Type: application/json` (when there is a body),
`Accept: application/json` and the resolved `User-Agent`.

## 7. AuthOverride

```go
openapi.Options{
    AuthOverride: &openapi.AuthOverride{
        HeaderName:  "X-Auth-Token", // replaces "Authorization"; empty keeps the default
        TokenPrefix: "token",        // replaces "bearer"; "" sends the raw token
    },
}
```

It affects **only bearer-style credentials** — the `bearerToken` scheme path and the
no-schemes fallback. It does nothing to apiKey-header schemes (those already send the exact
header the spec names) or basic auth.

Use it when:

- the API wants `Authorization: token <value>` (GitHub-style) → `TokenPrefix: "token"`
- the API wants the raw token with no prefix → `TokenPrefix: ""` (an explicit empty string is
  distinguishable from "unset" because the option is passed as a pointer internally)
- the spec declares the credential on `Authorization` but the API actually reads a different
  header → `HeaderName: "X-Auth-Token"`
- the spec declares no security scheme at all and the fallback `bearerToken` needs shaping

Do **not** reach for it just because the API uses a non-`Authorization` header: if the spec
declares that header as an `apiKey` scheme, discovery already sends it correctly. Confirm
with the preflight harness, which prints `config var "x" -> header "y"` per scheme.

The credential value itself always comes from the user at runtime via `pulumi config set`;
never bake a token into `Options`.

## 8. Polling

After create, the provider polls the read endpoint until the resource exists, then re-reads
to populate state; after delete it polls until the read 404s. Defaults: 5 min timeout, 1 s
initial interval, ×1.5 backoff, 30 s cap.

```go
openapi.Options{
    PollingOptions: openapi.PollingOptions{Timeout: 30 * time.Second, MaxInterval: 5 * time.Second},
}
```

Turn it off with `DisablePolling: true` when the API is strongly consistent and the extra
GET per create is unwanted, or when the read endpoint is expensive or rate-limited. Note the
tradeoff: with polling on, create state comes from a fresh read (more accurate); with it off,
create state is whatever the create response returned.
