# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build and test (prefer make targets)
make build
make test
make lint

# Tidy all go.mod files in the repo
make tidy

# Build all example provider binaries (output: bin/examples/)
make build-examples

# Generate schema.json for all examples
make schema

# Generate SDKs for all examples
make gen-sdk

# Remove built binaries and coverage output
make clean

# Run a single test without make
go test ./spec/... -v -run TestDiscover_Petstore
```

The examples each have their own `go.mod` with a `replace` directive pointing at the repo root. Run `make tidy` (or `go mod tidy` inside the example directory) if dependencies drift.

## Architecture

This is a Go library that lets engineers build Pulumi native providers from OpenAPI/Swagger specs at runtime — no code generation. It wraps [`pulumi-go-provider`](https://github.com/pulumi/pulumi-go-provider).

### Data flow

```
Options{SpecURL/SpecPath} 
  → spec.Load()                    # fetches/reads the spec via libopenapi
  → spec.Discover()                # groups paths into ResourceDefs by convention;
                                   # also extracts AuthSchemes from securityDefinitions/securitySchemes
  → spec.BuildSchema()             # emits Pulumi PackageSpec JSON; config vars are derived
                                   # from AuthSchemes (falls back to generic vars if none declared)
  → runtime.Build()                # assembles a p.Provider with CRUD dispatch
  → infer.NewProviderBuilder().WithWrapped(dynProvider)  # layers infer on top
```

### Resource discovery convention (`spec/resource.go`)

`Discover` routes to `discoverV2` (Swagger 2.0) or `discoverV3` (OAS3) based on `libopenapi.Document.GetSpecInfo().SpecFormat` — the constant is lowercase `"oas2"`, not `"OAS2"`.

`groupPathStrings` is the shared core. For every path ending in `{param}`, that path is an **item** and its parent (all segments except the last) is the **collection**. Paths are processed deepest-first; once a path is claimed as a collection it cannot also appear as a shallower item. This handles both simple paths (`/pet/{petId}`) and org-scoped paths (`/api/orgs/{orgName}/tokens/{tokenId}`).

Resource names are derived by CamelCase-joining the **static** (non-`{param}`) segments of the collection path.

"Context params" — `{param}` placeholders in the item path other than the trailing ID param (e.g. `{orgName}`) — are injected as required string inputs on the resource so users can provide them.

### Runtime dispatch (`runtime/`)

`runtime.Build()` returns a `p.Provider` struct (function-field based) keyed by Pulumi token. `tokenFromURN` extracts the token from the full URN for routing.

`substituteAllParams` in `crud.go` replaces all `{param}` placeholders in a URL path: the resource ID first, then any remaining placeholders from the inputs/state map. This is how context params like `{orgName}` get filled in at operation time.

### infer integration (`provider.go`)

`ProviderBuilder.Build()` calls `pb.inner.BuildOptions()` then `infer.Provider(opts)` directly — bypassing `infer.ProviderBuilder.Build()` which requires at least one infer resource. The dynamic dispatch provider is composed via `infer.NewProviderBuilder().WithWrapped(dynProvider)`. Schema merging happens automatically: infer's `schema.Wrap` calls the wrapped provider's `GetSchema` and merges both.

### Key types

- `spec.ResourceDef` — everything needed to CRUD a resource: paths, methods, ID param, input/output schemas
- `spec.AuthScheme` — a single security scheme discovered from the spec: kind (`"apiKey"`, `"bearer"`, `"basic"`), config var name, header/query param name
- `spec.DiscoveryResult` — slice of `ResourceDef` + shared types map + default base URL + `[]AuthScheme`
- `config.AuthScheme` — runtime mirror of `spec.AuthScheme`; held by `ProviderConfig` to drive `Apply` and `AuthHeaders`
- `config.ProviderConfig` — thread-safe holder for base URL and scheme values; `Apply` reads config vars named by each scheme; `AuthHeaders` builds the HTTP header map
- `openapi.Options` / `openapi.ResourceOverride` — public API surface for provider authors

### Reference

- OpenAPI specification: https://swagger.io/specification/

### libopenapi notes

- Ordered maps are iterated with `.Oldest()` / `.Next()` (v2) or range over `.FromOldest()` (v3)
- `SchemaProxy.GetReference()` returns the `$ref` string before resolution; call `.Schema()` to get the resolved schema
- V2 definitions live under `#/definitions/`; V3 schemas live under `#/components/schemas/`
