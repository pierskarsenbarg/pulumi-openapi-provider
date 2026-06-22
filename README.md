# pulumi-openapi-provider

A Go framework for building [Pulumi](https://www.pulumi.com) native providers from OpenAPI/Swagger specs — with no code generation required.

Built on top of [`pulumi-go-provider`](https://github.com/pulumi/pulumi-go-provider). The framework parses your spec at runtime, discovers resources by convention, maps OpenAPI schemas to Pulumi property types, and wires up HTTP CRUD dispatch automatically.

## How it works

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

End-to-end integration tests live in [`integration-tests/`](integration-tests/). They spin up a local HTTP API, build a provider from its OpenAPI spec, and run a Pulumi program against it.

### API

The test target is a [Hono](https://hono.dev) API running on [Bun](https://bun.sh), backed by SQLite via [Drizzle ORM](https://orm.drizzle.team). It exposes four resources that cover both flat and nested (context-param) path patterns:

| Resource | Endpoints |
|---|---|
| User | `POST /users`, `GET/PATCH/DELETE /users/:userId` |
| Organisation | `POST /organisations`, `GET/PATCH/DELETE /organisations/:organisationId` |
| Team | `POST /organisations/:organisationId/teams`, `GET/PATCH/DELETE /organisations/:organisationId/teams/:teamId` |
| Member | `POST /organisations/:organisationId/teams/:teamId/members`, `GET/DELETE …/members/:memberId` |

The API serves its own OpenAPI spec at `GET /openapi`, which is what the provider consumes.

### Running

```bash
# Terminal 1 — start the API
cd integration-tests && make run-api

# Terminal 2 — run the full test suite (pulumi up + destroy)
cd integration-tests && make test
```
