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

If neither the spec nor `--base-url` provides a base URL, the command exits with a clear error.

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

| Spec scheme type         | Generated config variable                | HTTP effect                     |
| ------------------------ | ---------------------------------------- | ------------------------------- |
| `apiKey` in header       | secret string named after the scheme key | sets the declared header        |
| `http` bearer / `oauth2` | `bearerToken` (secret string)            | `Authorization: Bearer <value>` |
| `http` basic             | `username` + `password` (secret)         | `Authorization: Basic <base64>` |

`baseUrl` is always available to override the server URL from the spec.

If the spec declares no security schemes the framework falls back to generic `apiKey`, `apiKeyHeader`, and `bearerToken` variables.

Pulumi users configure the provider in the usual way:

```bash
pulumi config set myprovider:bearerToken mytoken --secret
pulumi config set myprovider:baseUrl https://api.example.com
```

To supply a fixed base URL at build time rather than leaving it to users, set `Options.BaseURL`.

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
        // Provide an update endpoint that uses a body ID instead of a path param
        "Pet": {
            UpdatePath:   "/pet/{petId}",
            UpdateMethod: "PUT",
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

## Examples

- [`examples/petstore`](examples/petstore) — provider built from the [Swagger Petstore](https://petstore.swagger.io) spec (Swagger 2.0)
- [`examples/intercom`](examples/intercom) — provider built from the [Intercom API](https://github.com/intercom/Intercom-OpenAPI) spec (OAS3)

## Integration tests

End-to-end integration tests live in [`integration-tests/`](integration-tests/). They spin up a shared local HTTP API and run two independent test suites against it — one for each usage mode.

### API

The test target is a [Hono](https://hono.dev) API running on [Bun](https://bun.sh), backed by SQLite via [Drizzle ORM](https://orm.drizzle.team). It exposes four resources covering both flat and nested (context-param) path patterns:

| Resource | Endpoints |
|---|---|
| User | `POST /users`, `GET/PATCH/DELETE /users/:userId` |
| Organisation | `POST /organisations`, `GET/PATCH/DELETE /organisations/:organisationId` |
| Team | `POST /organisations/:organisationId/teams`, `GET/PATCH/DELETE /organisations/:organisationId/teams/:teamId` |
| Member | `POST /organisations/:organisationId/teams/:teamId/members`, `GET/DELETE …/members/:memberId` |

The API serves its own OpenAPI spec at `GET /openapi`.

### Code-provider suite (`integration-tests/code-provider/`)

Tests library mode. The Go provider binary has the spec URL baked in at compile time. A TypeScript Pulumi program creates and destroys all four resource types to verify end-to-end CRUD.

### Parameterized-provider suite (`integration-tests/parameterized-provider/`)

Tests parameterized binary mode. The `pulumi-resource-openapi-provider` binary is installed as a Pulumi plugin, then `pulumi package add openapi-provider http://localhost:3000/openapi` fetches the spec at runtime, derives a package name (`integration-test-api`) from the API title, and generates a typed TypeScript SDK. The same CRUD Pulumi program then runs against the generated SDK.

### Running

```bash
# Terminal 1 — start the API (shared by both suites)
cd integration-tests && make run-api

# Terminal 2 — run both suites (code-provider, then parameterized-provider)
cd integration-tests && make test

# Or run each suite independently
cd integration-tests && make test-code-provider
cd integration-tests && make test-parameterized-provider
```
