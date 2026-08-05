# pulumi-openapi-provider

A Go framework for building [Pulumi](https://www.pulumi.com) native providers from OpenAPI/Swagger specs — with no code generation required.

Built on top of [`pulumi-go-provider`](https://github.com/pulumi/pulumi-go-provider). The framework parses your spec at runtime, discovers resources by convention, maps OpenAPI schemas to Pulumi property types, and wires up HTTP CRUD dispatch automatically.

There are two ways to use this project:

- **[Parameterized binary](#pulumi-package-add-no-code-setup)** — install `pulumi-resource-openapi-provider` once and point it at any spec. No Go code required.
- **[Go library](#go-library)** — import the package and build your own provider binary when you need custom resources, overrides, or metadata.

---

## `pulumi package add` — no-code setup

Install the `pulumi-resource-openapi-provider` binary as a Pulumi plugin:

```bash
pulumi plugin install resource openapi-provider v0.1.0 \
  --server github://api.github.com/pierskarsenbarg/pulumi-openapi-provider
```

Then generate a typed SDK for any OpenAPI spec in one command:

```bash
pulumi package add openapi-provider 'https://api.example.com/openapi.json'
```

This calls the Parameterize RPC on the binary, which:

1. Fetches and parses the spec
2. Derives a package name from `info.title` (e.g. `"Petstore API"` → `"petstore-api"`) and a semver version from `info.version`
3. Discovers resources using the same path-convention logic as the library
4. Returns a schema with a `parameterization` block that embeds the spec URL; generated SDKs carry this blob so re-parameterization is automatic

The generated SDK and a `sdk-<language>/` directory appear in your project, ready to use.

### Base URL

If the spec declares a `servers[0].url` (OAS3) or `host` + `basePath` (Swagger 2.0) those values are used automatically. When the spec has no server address, or you want to override it, pass `--base-url`:

```bash
pulumi package add openapi-provider 'https://api.example.com/openapi.json' \
  --base-url=https://api.example.com
```

If neither the spec nor `--base-url` provides a base URL, `pulumi package add` still succeeds — the SDK generates fine, and the failure surfaces on the first API call as `baseUrl is not set: provide it via provider config or ensure the spec declares a server URL`. Users can also supply it at deploy time with `pulumi config set <package-name>:baseUrl https://api.example.com`.

### Provider configuration

After SDK generation, configure the provider the same way as the library-based approach (see [Provider configuration](#provider-configuration) below):

```bash
pulumi config set openapi-provider:bearerToken mytoken --secret
pulumi config set openapi-provider:baseUrl https://api.example.com
```

---

## Go library

### How it works

The framework groups API paths by their static prefix, then detects CRUD operations by HTTP method and path shape:

| Pattern                                    | Operation |
| ------------------------------------------ | --------- |
| `POST /things`                             | Create    |
| `GET /things/{id}`                         | Read      |
| `PUT /things/{id}` or `PATCH /things/{id}` | Update    |
| `DELETE /things/{id}`                      | Delete    |

Each discovered group becomes a Pulumi resource. The path parameter on the Read/Delete endpoint (`{id}`) is used as the resource identifier.

A group is only emitted when it has a **create** operation plus **read or delete**. Groups that fail that test are dropped silently — no warning, no error, just a resource missing from the generated SDK. Update is optional: a resource without one is still emitted, and property changes then no-op rather than reaching the API.

Update is also only detected on the item path. These are *not* recognised, and need an override (see [Overriding convention-based behaviour](#overriding-convention-based-behaviour)):

- `PUT /things` or `PATCH /things` — update with the ID in the request body
- `POST /things/{id}` — the Swagger Petstore's form-encoded update
- any update under a different path, e.g. `/things/{id}/rename`

### Resource names and tokens

Resource names are a PascalCase join of the collection path's **static** segments; `{param}` segments are skipped and `-`, `_`, `.` split words. A leading `api` segment is stripped, and nothing else is:

| Collection path | Resource name |
| --------------- | ------------- |
| `/pet` | `Pet` |
| `/store/order` | `StoreOrder` |
| `/orgs/{orgId}/teams` | `OrgsTeams` |
| `/api/widgets` | `Widgets` |
| `/extras/gadgets` | `ExtrasGadgets` |

The Pulumi token is `<package>:<module>:<Name>`. The module is the first operation tag that also appears in the spec's root `tags` list, lowercased and stripped of non-alphanumerics (`"AI Content"` → `aicontent`); when there is no such tag it is `index`. So a spec declaring root tags produces `petstore:pet:Pet`, not `petstore:index:Pet` — which changes the import path in generated SDKs. Complex and enum types always live in `index`.

### Type mapping

OpenAPI schema types are mapped to Pulumi property types:

| OpenAPI type | Pulumi type |
| ------------ | ----------- |
| `string`     | `string`    |
| `integer`    | `integer`   |
| `number`     | `number`    |
| `boolean`    | `boolean`   |
| `array`      | `array`     |
| `object`     | `object`    |

Anything else — including a schema with no `type` — maps to `string`. Four limits worth knowing before relying on schema fidelity:

- An inline `type: object` becomes an opaque `object` with no properties. Only `$ref`'d schemas become named types with real fields, so hoisting an inline object into `components/schemas` and `$ref`ing it is what makes it typed.
- Arrays of arrays degrade to `string`, because the inner item type can't be represented.
- `format` (`date-time`, `int64`, `uuid`) is ignored; every property gets the base type.
- `allOf` / `anyOf` are not merged. A schema composed purely of `allOf` yields a type with no properties. (One `oneOf` case is handled: a property-less request body whose `oneOf` holds a single-object and a bulk-array variant uses the first non-array variant.)

**Enums** are fully supported for both Swagger 2.0 and OpenAPI 3.x. Named enum definitions (referenced via `$ref`) and inline enum values on properties are both registered as typed Pulumi enum types. The enum values' native types (string, integer, number, boolean) are preserved. Empty-string and `null` enum values are silently dropped, as they cannot produce valid SDK constant names — if a spec uses `""` to mean "unset", that state has no constant in the generated SDK and the property is better modelled as a plain string.

## Installation

```bash
go get github.com/pierskarsenbarg/pulumi-openapi-provider
```

## Quickstart

A minimal provider needs only a `main.go`:

```go
package main

import (
    "context"
    "log"

    openapi "github.com/pierskarsenbarg/pulumi-openapi-provider"
)

func main() {
    err := openapi.RunProvider(context.Background(), "myprovider", "0.1.0", openapi.Options{
        SpecURL: "https://api.example.com/openapi.json",
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

Build it as `pulumi-resource-myprovider` and it is a fully working Pulumi provider.

## Schema extraction and SDK generation

Because the provider implements the standard Pulumi provider protocol, the usual toolchain works out of the box:

```bash
# Extract schema.json
pulumi package get-schema ./pulumi-resource-myprovider > schema.json

# Generate SDKs (from the provider binary directly)
pulumi package gen-sdk ./pulumi-resource-myprovider --language all --out sdk
```

To emit `schema.json` without running the provider (e.g. in CI):

```go
schema, err := openapi.GetSchema("myprovider", "0.1.0", openapi.Options{
    SpecURL: "https://api.example.com/openapi.json",
})
if err != nil {
    log.Fatal(err)
}
os.WriteFile("schema.json", []byte(schema), 0o644)
```

## Provider metadata

Use the builder API to set metadata before running:

```go
builder, err := openapi.NewProviderBuilder("myprovider", "0.1.0", openapi.Options{
    SpecURL: "https://api.example.com/openapi.json",
})
if err != nil {
    log.Fatal(err)
}

provider, err := builder.
    WithDescription("Pulumi provider for Example API").
    WithHomepage("https://example.com").
    WithRepository("https://github.com/myorg/pulumi-myprovider").
    WithLicense("Apache-2.0").
    WithPluginDownloadURL("https://github.com/myorg/pulumi-myprovider/releases/download/${VERSION}").
    Build()
if err != nil {
    log.Fatal(err)
}

p.RunProvider(context.Background(), "myprovider", "0.1.0", provider)
```

## Provider configuration

The framework derives provider configuration variables automatically from the spec's `securityDefinitions` (Swagger 2.0) or `components/securitySchemes` (OAS3):

| Spec scheme type                        | Generated config variable                 | HTTP effect                       |
| --------------------------------------- | ----------------------------------------- | --------------------------------- |
| `apiKey` in header                      | scheme key, first letter lowercased (secret) | sets the declared header       |
| `apiKey` in header, named `Authorization` | scheme key, first letter lowercased (secret) | `Authorization: bearer <value>` |
| `apiKey` in query                       | scheme key, first letter lowercased (secret) | **nothing — see below**         |
| `http` bearer / `oauth2` / `openIdConnect` | `bearerToken` (secret)                 | `Authorization: bearer <value>`   |
| `http` basic                            | `username` + `password` (secret)          | `Authorization: Basic <base64>`   |

Two things to watch:

- Config variables for `apiKey` schemes are named after the **scheme key in the spec**, not the header, and only the first character is lowercased. A scheme keyed `X-Auth-Token` produces the config variable `x-Auth-Token`, so users run `pulumi config set myprovider:x-Auth-Token <value> --secret`. Only `http bearer`, `oauth2` and `openIdConnect` produce a variable literally named `bearerToken`.
- **`apiKey` in query is not yet sent.** The variable is generated and accepted, but no query parameter is added to requests, so calls go out unauthenticated and the API answers 401 with nothing in the provider output pointing at the cause. Until this is supported, use a custom `Options.HTTPClient` whose `RoundTripper` appends the parameter.

The default bearer prefix is lowercase `bearer`. Use `Options.AuthOverride` to change it (see [Non-standard auth conventions](#non-standard-auth-conventions)).

`baseUrl` is always available to override the server URL from the spec.

If the spec declares no security schemes the framework falls back to generic `apiKey`, `apiKeyHeader`, and `bearerToken` variables.

Pulumi users configure the provider in the usual way:

```bash
pulumi config set myprovider:bearerToken mytoken --secret
pulumi config set myprovider:baseUrl https://api.example.com
```

To supply a fixed base URL at build time rather than leaving it to users, set `Options.BaseURL`.

### Non-standard auth conventions

Some APIs accept a token but don't follow standard header or prefix conventions — for example wanting `token <value>` instead of `bearer <value>`, or reading a header the spec doesn't declare. Use `Options.AuthOverride` to handle this at build time (library mode only; not available in the parameterized provider):

```go
openapi.Options{
    SpecURL: "https://api.example.com/openapi.json",
    AuthOverride: &openapi.AuthOverride{
        HeaderName:  "X-Auth-Token", // default: "Authorization"
        TokenPrefix: "token",        // default: "bearer"; set to "" to send the raw token
    },
}
```

Both fields are optional — set only the ones you need. The credential value itself is always supplied by the end-user via `pulumi config set` at runtime.

`AuthOverride` affects **bearer-style credentials only**: the `bearerToken` path, an `apiKey` scheme declared on the `Authorization` header, and the no-schemes fallback. It does nothing to an `apiKey` scheme that names its own header — a spec declaring `apiKey in: header, name: X-Auth-Token` already sends exactly that header, so no override is needed.

## Other options

| Option | Purpose |
| ------ | ------- |
| `ExcludeTags` | Skip every resource whose CRUD operations carry one of these operation tags. The lever for trimming large specs down to the resources you actually want to ship. |
| `HTTPClient` | Custom `*http.Client` for both the spec fetch and API calls — the hook for mTLS, kubeconfig transports, signing round-trippers and proxies. See [`examples/openapi-k8s`](examples/openapi-k8s). |
| `UserAgent` | Replaces the default `pulumi-openapi-provider/<version>` header sent with every request. |
| `DisablePolling` | Skip the post-create "wait until it exists" and post-delete "wait until it's gone" reads. |
| `PollingOptions` | Tune that polling: `Timeout` (default 5 min), `InitialInterval` (1 s), `MaxInterval` (30 s), `Multiplier` (1.5). |

```go
openapi.Options{
    SpecURL:        "https://api.example.com/openapi.json",
    ExcludeTags:    []string{"internal", "beta"},
    PollingOptions: openapi.PollingOptions{Timeout: 30 * time.Second},
}
```

With polling enabled (the default) the state recorded after create comes from a fresh read of the resource; with `DisablePolling` it is whatever the create response returned.

## Adding resources not in the spec

Use `WithResources` to add hand-crafted [`infer`](https://github.com/pulumi/pulumi-go-provider/tree/main/infer) resources alongside the spec-derived ones:

```go
builder, err := openapi.NewProviderBuilder("myprovider", "0.1.0", openapi.Options{
    SpecURL: "https://api.example.com/openapi.json",
})

provider, err := builder.
    WithResources(infer.Resource[WidgetArgs, WidgetState]()).
    Build()
```

## Overriding convention-based behaviour

When an API doesn't follow standard REST conventions, use `ResourceOverride`:

```go
openapi.Options{
    SpecURL: "https://api.example.com/openapi.json",
    Overrides: map[string]openapi.ResourceOverride{
        // Wire up an update endpoint discovery didn't find, where the ID is in the URL
        "Pet": {
            UpdatePath:   "/pet/{petId}",
            UpdateMethod: "PUT",
        },
        // Read the resource ID from a differently-named response field
        "Org": {
            IDField: "org_id",
        },
        // Rename a resource's Pulumi token
        "InventoryItem": {
            Token: "myprovider:index:Item",
        },
        // Exclude a path group from discovery entirely
        "InternalResource": {
            Skip: true,
        },
    },
}
```

Keys are the **discovered** resource names (`"Pet"`, `"StoreOrder"`, `"OrgsTeams"`), not tokens. A key that matches nothing is ignored silently.

| Field                         | Description                                                      |
| ----------------------------- | ---------------------------------------------------------------- |
| `Skip`                        | Exclude this resource from discovery                             |
| `Token`                       | Override the generated Pulumi token                              |
| `CreatePath` / `CreateMethod` | Override the create endpoint                                     |
| `ReadPath`                    | Override the read endpoint                                       |
| `UpdatePath` / `UpdateMethod` | Override the update endpoint                                     |
| `DeletePath`                  | Override the delete endpoint                                     |
| `IDPathParam`                 | Override the path parameter name used as the resource ID         |
| `IDField`                     | Override the JSON response field used to extract the resource ID |
| `Check` / `Diff`              | Replace input validation / diff computation for this resource    |
| `Create` / `Read` / `Update` / `Delete` | Replace the generated HTTP call for one operation       |

### The `IDField` override

At create time the resource ID is pulled out of the JSON response by looking for `IDField`, then `IDPathParam`, then `"id"`, using the API's own property names (dot notation traverses nested objects, e.g. `metadata.name`). If none is present, create fails with `could not extract ID from response (looked for field "…")`. A response of `{"org_id": …}` therefore needs `IDField: "org_id"` — the camelCased `orgId` will not match.

### Function hooks

The same struct carries optional functions that replace the generated behaviour for one resource. Any nil hook falls back to the built-in implementation, so hooks are for the one operation an API does strangely rather than an all-or-nothing takeover:

```go
Overrides: map[string]openapi.ResourceOverride{
    "User": {
        Check: func(ctx context.Context, req p.CheckRequest) (p.CheckResponse, error) {
            email, ok := req.Inputs.GetOk("email")
            if ok && email.IsString() && !strings.Contains(email.AsString(), "@") {
                return p.CheckResponse{Inputs: req.Inputs, Failures: []p.CheckFailure{
                    {Property: "email", Reason: "email must contain @"}}}, nil
            }
            return p.CheckResponse{Inputs: req.Inputs}, nil
        },
    },
}
```

A hook receives only the Pulumi request — it has no handle on the resolved base URL or credentials, so a hook that calls the API must bring its own client and configuration.

**Body-ID updates need a hook, not a path override.** `id` is stripped from the input schema during discovery (Pulumi reserves it), so an override pointing update at `PUT /things` would send a body with no ID — and APIs that treat an ID-less PUT as a create will duplicate the resource rather than erroring. Either use an `Update` hook that re-injects the ID, or override `UpdatePath` to the item path and rewrite the request in a `RoundTripper` on `Options.HTTPClient`, which keeps the provider's configured base URL and auth headers.

### The wildcard key

`Overrides["*"]` applies to every resource as a baseline, and a named entry wins field by field:

```go
Overrides: map[string]openapi.ResourceOverride{
    "*":       {IDField: "metadata.name"},
    "Configs": {IDField: "uid"},
}
```

`Skip` is not read from the wildcard — `"*": {Skip: true}` disables nothing.

## Examples

- [`examples/petstore`](examples/petstore) — provider built from the [Swagger Petstore](https://petstore.swagger.io) spec (Swagger 2.0)
- [`examples/intercom`](examples/intercom) — provider built from the [Intercom API](https://github.com/intercom/Intercom-OpenAPI) spec (OAS3)