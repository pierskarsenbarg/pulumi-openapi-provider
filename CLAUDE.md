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

# Generate schema.json for all examples (output: examples/*/schema.json — gitignored)
make schema

# Generate SDKs for all examples
make gen-sdk

# Remove built binaries and coverage output
make clean

# Run a single test without make
go test ./pkg/spec/... -v -run TestDiscover_Petstore
```

The examples each have their own `go.mod` with a `replace` directive pointing at the repo root. Run `make tidy` (or `go mod tidy` inside the example directory) if dependencies drift.

## Architecture

This is a Go library that lets engineers build Pulumi native providers from OpenAPI/Swagger specs at runtime — no code generation. It wraps [`pulumi-go-provider`](https://github.com/pulumi/pulumi-go-provider).

The repo ships two distinct usage modes:

1. **Library mode** (`provider.go`) — provider authors import the package and call `NewProviderBuilder` / `RunProvider`. Spec URL is known at compile time.
2. **Parameterized binary mode** (`pkg/parameterized/`, `cmd/openapi-provider/`) — a single `pulumi-resource-openapi-provider` binary that accepts the spec URL at runtime via the Parameterize RPC. Users run `pulumi package add openapi-provider '<url>'` and get a typed SDK with no Go code required.

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

### Resource discovery convention (`pkg/spec/resource.go`)

`Discover` routes to `discoverV2` (Swagger 2.0) or `discoverV3` (OAS3) based on `libopenapi.Document.GetSpecInfo().SpecFormat` — the constant is lowercase `"oas2"`, not `"OAS2"`.

`groupPathStrings` is the shared core. For every path ending in `{param}`, that path is an **item** and its parent (all segments except the last) is the **collection**. Paths are processed deepest-first; once a path is claimed as a collection it cannot also appear as a shallower item. This handles both simple paths (`/pet/{petId}`) and org-scoped paths (`/api/orgs/{orgName}/tokens/{tokenId}`).

Resource names are derived by CamelCase-joining the **static** (non-`{param}`) segments of the collection path.

"Context params" — `{param}` placeholders in the item path other than the trailing ID param (e.g. `{orgName}`) — are injected as required string inputs on the resource so users can provide them.

#### Enum support

Both `typeCollector` (V2) and `typeCollectorV3` (V3) handle enums in two places:

- **Named enums** (`ensureType`): if a definition/component schema has `Enum` values and no `Properties`, it is registered as a `pschema.ComplexTypeSpec` with `Enum: [...]` instead of an object type. The `Type` field is set to the Pulumi equivalent of the OpenAPI type (`pulumiTypeForOAPIType`).
- **Inline enums** (`convertSchema`): if a property schema has `Enum` values and a non-empty `typeHint` was passed by the caller, a named enum type is registered under `pkgname:index:TypeHint` and the property returns a `$ref` instead of a primitive type. The hint is derived from `ResourceName + PascalCase(propertyName)` (for resource-level properties) or `TypeName + PascalCase(propertyName)` (for nested object properties).

`extractEnumValues` converts `[]*yaml.Node` → `[]pschema.EnumValueSpec`, using the YAML `Tag` field (`!!int`, `!!float`, `!!bool`, default `!!str`) to preserve native value types. Empty-string values are skipped — they produce an unnamed Go constant that collides with the type name during SDK generation.

### Runtime dispatch (`pkg/runtime/`)

`runtime.Build()` returns a `p.Provider` struct (function-field based) keyed by Pulumi token. `tokenFromURN` extracts the token from the full URN for routing.

`substituteAllParams` in `crud.go` replaces all `{param}` placeholders in a URL path: the resource ID first, then any remaining placeholders from the inputs/state map. This is how context params like `{orgName}` get filled in at operation time.

### infer integration (`provider.go`)

`ProviderBuilder.Build()` calls `pb.inner.BuildOptions()` then `infer.Provider(opts)` directly — bypassing `infer.ProviderBuilder.Build()` which requires at least one infer resource. The dynamic dispatch provider is composed via `infer.NewProviderBuilder().WithWrapped(dynProvider)`. Schema merging happens automatically: infer's `schema.Wrap` calls the wrapped provider's `GetSchema` and merges both.

### Parameterized provider (`pkg/parameterized/parameterized.go`)

`parameterizedProvider` holds a mutex-protected `*paramState` (nil until `Parameterize` fires). All provider function methods (`getSchema`, `configure`, `check`, `create`, etc.) call `getState()` first and delegate to `paramState.inner`. This avoids any infer dependency for the parameterized path — the provider is wired directly as a `p.Provider` struct.

`paramBlob` (JSON: `{"specURL":"…","baseURL":"…"}`) is the round-trip value embedded in `PackageSpec.Parameterization.Parameter`. `baseURL` is only populated when `--base-url` was explicitly supplied; an empty value means "re-derive from spec on next Parameterize".

`spec.BaseURL(doc)` is the exported counterpart to the internal `extractBaseURLV2`/`extractBaseURLV3` helpers. It returns empty string when the spec has no declared server address, rather than defaulting to `localhost`. Used by the parameterized path to decide whether a `--base-url` flag is required.

### Key types

- `spec.ResourceDef` — everything needed to CRUD a resource: paths, methods, ID param, input/output schemas
- `spec.AuthScheme` — a single security scheme discovered from the spec: kind (`"apiKey"`, `"bearer"`, `"basic"`), config var name, header/query param name
- `spec.DiscoveryResult` — slice of `ResourceDef` + shared types map + default base URL + `[]AuthScheme`
- `config.AuthScheme` — runtime mirror of `spec.AuthScheme`; held by `ProviderConfig` to drive `Apply` and `AuthHeaders`
- `config.ProviderConfig` — thread-safe holder for base URL, scheme values, and optional auth overrides; `Apply` reads config vars named by each scheme; `AuthHeaders` builds the HTTP header map (respects `authHeaderOverride` / `tokenPrefixOverride` when set)
- `openapi.Options` / `openapi.ResourceOverride` / `openapi.AuthOverride` — public API surface for library provider authors
- `parameterized.parameterizedProvider` / `parameterized.paramState` — internal types for the parameterized binary; not part of the library API

### Auth overrides (`openapi.AuthOverride`)

`Options.AuthOverride` is a library-mode-only escape hatch for APIs that deviate from standard bearer auth conventions. It has no effect on the parameterized provider.

```go
openapi.Options{
    AuthOverride: &openapi.AuthOverride{
        HeaderName:  "X-Auth-Token", // replaces "Authorization"; leave empty to keep default
        TokenPrefix: "token",        // replaces "bearer"; set to "" for no prefix
    },
}
```

`config.New` receives the resolved header and prefix as `authHeaderOverride string` / `tokenPrefixOverride *string` (pointer so an empty string is distinguishable from "not set"). The helpers `bearerHeader()` / `bearerValue()` on `ProviderConfig` centralise the logic and are called from both the scheme-based bearer path and the legacy fallback `bearerToken` path in `AuthHeaders()`.

### Reference

- OpenAPI specification: https://swagger.io/specification/

### libopenapi notes

- Ordered maps are iterated with `.Oldest()` / `.Next()` (v2) or range over `.FromOldest()` (v3)
- `SchemaProxy.GetReference()` returns the `$ref` string before resolution; call `.Schema()` to get the resolved schema
- V2 definitions live under `#/definitions/`; V3 schemas live under `#/components/schemas/`
- `schema.Enum` is `[]*yaml.Node` (from `go.yaml.in/yaml/v4`); use `n.Tag` to distinguish `!!int` / `!!float` / `!!bool` / `!!str` and `n.Value` for the string representation

## Integration tests

The integration tests live under `integration-tests/` and are split into two independent suites that share a single API:

```
integration-tests/
├── api/                          # shared Hono/Bun HTTP API
├── code-provider/
│   ├── provider/                 # Go binary built from the spec at compile time
│   └── pulumi/                   # Pulumi TypeScript program for code-provider tests
└── parameterized-provider/
    └── pulumi/                   # Pulumi TypeScript program for parameterized-provider tests
```

### Integration test API

The API is built with [Hono](https://hono.dev) running on [Bun](https://bun.sh), backed by SQLite via [Drizzle ORM](https://orm.drizzle.team), and serves an OpenAPI spec at `/openapi` via [hono-openapi](https://hono-openapi.vercel.app).

Resources exposed:

| Route | Context param | Resource |
|---|---|---|
| `POST /users`, `GET/PATCH/DELETE /users/:userId` | — | User (name, email) |
| `POST /organisations`, `GET/PATCH/DELETE /organisations/:organisationId` | — | Organisation (name) |
| `POST /organisations/:organisationId/teams`, `GET/PATCH/DELETE /organisations/:organisationId/teams/:teamId` | `organisationId` | Team (name) |
| `POST /organisations/:organisationId/teams/:teamId/members`, `GET/DELETE /organisations/:organisationId/teams/:teamId/members/:memberId` | `organisationId`, `teamId` | Member (userId) |

### Code-provider tests

Tests library mode: a Go binary with the spec URL baked in at compile time.

```bash
# In one terminal — start the API (blocks)
cd integration-tests && make run-api

# In another terminal
cd integration-tests && make test-code-provider
```

Individual targets:

```bash
make build-code-provider  # compile Go binary → code-provider/bin/pulumi-resource-testapi
make schema               # extract schema.json from the running provider
make sdk                  # generate TypeScript SDK from schema
make install-code-sdks    # install Pulumi program dependencies
```

### Parameterized-provider tests

Tests parameterized binary mode: the root `pulumi-resource-openapi-provider` binary is installed as a Pulumi plugin, then `pulumi package add` is used to fetch the spec at runtime and generate a typed SDK on the fly.

```bash
# In one terminal — start the API (blocks)
cd integration-tests && make run-api

# In another terminal
cd integration-tests && make test-parameterized-provider
```

What `make test-parameterized-provider` does:
1. Builds `bin/pulumi-resource-openapi-provider` from `cmd/openapi-provider/` in the repo root
2. Installs it as a Pulumi plugin (`pulumi plugin install resource openapi-provider 0.1.0`)
3. Runs `pulumi package add openapi-provider http://localhost:3000/openapi` to fetch the spec and generate a typed SDK (package name `integration-test-api`, derived from the API title)
4. Runs `pulumi install` to install the generated SDK
5. Runs `pulumi up` / `pulumi destroy` and removes the stack

### Running both suites

```bash
# In one terminal — start the API
cd integration-tests && make run-api

# In another terminal — run both suites in sequence
cd integration-tests && make test
```

### Shared API targets

```bash
make install-api  # install Bun dependencies
make generate     # generate Drizzle migration files from schema
make migrate      # run DB migrations
make clean        # remove all build artefacts including DB and node_modules
```

### Modifying the API schema

If you change the Drizzle schema (`integration-tests/api/src/db/schema.ts`), regenerate and apply migrations:

```bash
cd integration-tests && make generate migrate
```
