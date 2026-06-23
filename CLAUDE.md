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

# Build the standalone parameterized provider binary (output: bin/pulumi-resource-openapi-provider)
make build-provider

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

The repo ships two distinct usage modes:

1. **Library mode** (`provider.go`) — provider authors import the package and call `NewProviderBuilder` / `RunProvider`. Spec URL is known at compile time.
2. **Parameterized binary mode** (`parameterized.go`, `cmd/openapi-provider/`) — a single `pulumi-resource-openapi-provider` binary that accepts the spec URL at runtime via the Parameterize RPC. Users run `pulumi package add openapi-provider '<url>'` and get a typed SDK with no Go code required.

### Library data flow

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

### Parameterized binary data flow

```
Parameterize(Args: ["https://spec.url", "--base-url=https://api.url"])
  → parseParamArgs()               # splits spec URL and optional --base-url flag
  → spec.Load(specURL)             # fetches the spec
  → spec.BaseURL(doc)              # extracts server URL from spec (empty if not declared)
  → slugifyTitle(info.title)       # derives package name, e.g. "Petstore API" → "petstore-api"
  → normalizeVersion(info.version) # normalises to semver X.Y.Z
  → spec.Discover()                # same discovery as library mode
  → runtime.Build()                # builds the CRUD dispatch provider; stored as paramState.inner
  → ParameterizeResponse{Name, Version}

GetSchema()
  → paramState.inner.GetSchema()   # returns schema built during Parameterize
  → injects PackageSpec.Parameterization{BaseProvider, Parameter: blob}
                                   # blob = JSON{specURL, baseURL}; embedded in generated SDKs
                                   # and echoed back as ParameterizeRequestValue.Value on re-use
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

### Parameterized provider (`parameterized.go`)

`parameterizedProvider` holds a mutex-protected `*paramState` (nil until `Parameterize` fires). All provider function methods (`getSchema`, `configure`, `check`, `create`, etc.) call `getState()` first and delegate to `paramState.inner`. This avoids any infer dependency for the parameterized path — the provider is wired directly as a `p.Provider` struct.

`paramBlob` (JSON: `{"specURL":"…","baseURL":"…"}`) is the round-trip value embedded in `PackageSpec.Parameterization.Parameter`. `baseURL` is only populated when `--base-url` was explicitly supplied; an empty value means "re-derive from spec on next Parameterize".

`spec.BaseURL(doc)` is the exported counterpart to the internal `extractBaseURLV2`/`extractBaseURLV3` helpers. It returns empty string when the spec has no declared server address, rather than defaulting to `localhost`. Used by the parameterized path to decide whether a `--base-url` flag is required.

### Key types

- `spec.ResourceDef` — everything needed to CRUD a resource: paths, methods, ID param, input/output schemas
- `spec.AuthScheme` — a single security scheme discovered from the spec: kind (`"apiKey"`, `"bearer"`, `"basic"`), config var name, header/query param name
- `spec.DiscoveryResult` — slice of `ResourceDef` + shared types map + default base URL + `[]AuthScheme`
- `config.AuthScheme` — runtime mirror of `spec.AuthScheme`; held by `ProviderConfig` to drive `Apply` and `AuthHeaders`
- `config.ProviderConfig` — thread-safe holder for base URL and scheme values; `Apply` reads config vars named by each scheme; `AuthHeaders` builds the HTTP header map
- `openapi.Options` / `openapi.ResourceOverride` — public API surface for library provider authors
- `openapi.parameterizedProvider` / `openapi.paramState` — internal types for the parameterized binary; not part of the library API

### Reference

- OpenAPI specification: https://swagger.io/specification/

### libopenapi notes

- Ordered maps are iterated with `.Oldest()` / `.Next()` (v2) or range over `.FromOldest()` (v3)
- `SchemaProxy.GetReference()` returns the `$ref` string before resolution; call `.Schema()` to get the resolved schema
- V2 definitions live under `#/definitions/`; V3 schemas live under `#/components/schemas/`

## Integration tests

The integration tests live under `integration-tests/` and consist of two parts:

- `integration-tests/api/` — a Hono/Bun HTTP API that acts as the target for the provider
- `integration-tests/provider/` — a Pulumi provider built from the API's OpenAPI spec
- `integration-tests/pulumi/` — a Pulumi TypeScript program that exercises the provider end-to-end

### Integration test API

The API is built with [Hono](https://hono.dev) running on [Bun](https://bun.sh), backed by SQLite via [Drizzle ORM](https://orm.drizzle.team), and serves an OpenAPI spec at `/openapi` via [hono-openapi](https://hono-openapi.vercel.app).

Resources exposed:

| Route | Context param | Resource |
|---|---|---|
| `POST /users`, `GET/PATCH/DELETE /users/:userId` | — | User (name, email) |
| `POST /organisations`, `GET/PATCH/DELETE /organisations/:organisationId` | — | Organisation (name) |
| `POST /organisations/:organisationId/teams`, `GET/PATCH/DELETE /organisations/:organisationId/teams/:teamId` | `organisationId` | Team (name) |
| `POST /organisations/:organisationId/teams/:teamId/members`, `GET/DELETE /organisations/:organisationId/teams/:teamId/members/:memberId` | `organisationId`, `teamId` | Member (userId) |

### Running the integration tests

```bash
# In one terminal — start the API (blocks)
cd integration-tests && make run-api

# In another terminal — build provider, generate SDK, run pulumi up/destroy
cd integration-tests && make test
```

Individual targets:

```bash
make install-api    # install Bun dependencies
make generate       # generate Drizzle migration files from schema
make migrate        # run DB migrations
make build-provider # build the provider binary
make schema         # extract schema.json from a running provider
make sdk            # generate TypeScript SDK from schema
make install-sdks   # install Pulumi program dependencies
make clean          # remove all build artefacts including DB and node_modules
```

### Modifying the API schema

If you change the Drizzle schema (`integration-tests/api/src/db/schema.ts`), regenerate and apply migrations:

```bash
cd integration-tests && make generate migrate
```
