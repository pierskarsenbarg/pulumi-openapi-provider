---
name: pulumi-openapi-provider
description: Build a Pulumi provider from an OpenAPI/Swagger spec with pulumi-openapi-provider — no codegen. Covers the two modes (the parameterized `pulumi package add openapi-provider <spec-url>` binary, and the Go library with NewProviderBuilder/RunProvider), the path-shape conventions that decide which endpoints become resources, ResourceOverride/AuthOverride escape hatches, and provider config derived from securityDefinitions/securitySchemes. Use this whenever someone wants a Pulumi provider, package, or SDK for an HTTP API described by an OpenAPI/Swagger spec, mentions openapi-provider, `pulumi package add openapi-provider`, pulumi-openapi-provider, or is working in that repo — including when they just paste a spec URL and ask to "make this a Pulumi provider" or "get Pulumi resources for this API" without naming the library. Also use when a provider built this way misbehaves: a resource is missing from the generated SDK, create fails with "could not extract ID from response", auth headers are wrong, baseUrl is unset, or enum constants are missing.
---

# Pulumi providers from OpenAPI specs

`pulumi-openapi-provider` turns an OpenAPI/Swagger spec into a working Pulumi provider **at runtime**. It parses the spec, groups paths into resources by convention, maps schemas to Pulumi property types, and dispatches Pulumi CRUD to HTTP calls. Nothing is generated ahead of time, so the spec's shape *is* the provider's shape.

That is the thing to internalise: this library discovers resources by **path shape**, not by reading operationIds or intent. An endpoint that doesn't fit the shape is silently dropped — no warning, no error, just a resource missing from the SDK. Most of the work in a good build is predicting those drops *before* generating anything, which is what the preflight step below is for.

## Step 1 — Choose the mode

```
Does the job need ANY of:
  - hand-written Pulumi resources alongside the spec-derived ones (WithResources)
  - per-resource overrides (ResourceOverride: Skip / Token / paths / IDField / Check / Create hooks)
  - non-standard auth (AuthOverride: header name or token prefix)
  - a custom *http.Client (mTLS, kubeconfig transport, proxies)
  - excluding operations by tag (ExcludeTags), or polling tuning
  - provider metadata (description, homepage, license, publisher)
→ YES: library mode. Write a Go main.go. See "Library workflow".
→ NO:  parameterized mode. No Go code at all. See "Parameterized workflow".
```

If unsure, start parameterized — it costs one command. Switch to library mode the moment preflight predicts a gap that only an override can close, since the parameterized binary ignores `Overrides` and `AuthOverride` entirely (it calls `spec.Discover(doc, pkgName, nil, nil)`).

## Step 2 — Preflight the spec (do this before generating anything)

Skipping this is how people end up with a generated SDK missing half the API and no idea why. The goal is a short written prediction: which resources appear, which endpoints get dropped and why, what config variables the user will have to set, and which traps apply.

**Preferred: run real discovery.** Don't predict what the code will do when you can just ask it. `scripts/preflight/` in this skill is a ~40-line program that calls the library's own `spec.Discover` and prints every resource, path, ID field, input, enum type, and auth scheme:

```bash
# Inside the pulumi-openapi-provider checkout it runs as-is:
cd <skill-dir>/scripts/preflight && go run . <spec-url-or-file> [pkgname]

# Elsewhere, copy it out and point it at a module source:
cp -r <skill-dir>/scripts/preflight /tmp/preflight && cd /tmp/preflight
go mod edit -replace github.com/pierskarsenbarg/pulumi-openapi-provider=<path-to-checkout>
#   ...or drop the replace and use the published module:
#   go mod edit -dropreplace github.com/pierskarsenbarg/pulumi-openapi-provider
go mod tidy && go run . <spec-url-or-file> [pkgname]
```

It works on a URL or a local file, needs no API credentials, and its output is ground truth — same functions the provider runs. When the spec is behind auth or the network is closed, download the spec once and pass the file path.

**Fallback: walk the rules by hand.** Read `references/discovery-rules.md` and apply the predicates to the spec's path list. Do this when Go isn't available. It is slower and easier to get wrong, so prefer the harness.

**Either way, read `references/discovery-rules.md` at least once per build** — the harness tells you *what* happened, the reference explains *why*, which is what you need to recommend a fix.

Then report to the user, before writing code:

```
Resources discovered (N):
  Pet         POST /pet → GET|DELETE /pet/{petId}      id: "id"     no update endpoint
  StoreOrder  POST /store/order → GET|DELETE /...      id: "id"     no update endpoint
Endpoints NOT discovered:
  PUT /pet          — update by body ID; the library only reads update off the item path
  POST /pet/{petId}/uploadImage — action endpoint, not CRUD
  GET  /store/inventory         — collection GET only, no create
Provider config the user must set:
  petstore:baseUrl, petstore:api_key (secret), petstore:bearerToken (secret)
Traps that apply: <from the checklist below>
```

### Preflight checklist

Walk these; each one has burned a real build. Details and fixes in the references.

1. **No `POST /things` for a `GET /things/{id}`** → no resource at all. Create is mandatory; read *or* delete must also exist.
2. **Update lives somewhere other than `PUT|PATCH /things/{id}`** (e.g. `PUT /things` with the ID in the body, or `POST /things/{id}`) → the resource is discovered **update-free**, and changes silently no-op: `update` returns the inputs without calling the API and `pulumi up` still reports success. A path override works when the endpoint takes the ID in the URL; a true body-ID update needs an `Update` hook, because `id` is stripped from the inputs and would be missing from the request body. Either way → library mode.
3. **Create response doesn't return the ID under the expected key** → every create fails at runtime with `could not extract ID from response (looked for field "X")`. Especially common with snake_case (`org_id`) or nested IDs (`metadata.name`). Needs `ResourceOverride{IDField}` → library mode. This one is invisible until `pulumi up`, so call it out in preflight.
4. **Security scheme is `apiKey` `in: query`** → a config variable is generated but **never sent**; every call goes out unauthenticated. There is no override for this; see `references/overrides-and-auth.md`.
5. **The credential doesn't go where the spec says.** An `apiKey in: header` scheme is already sent on exactly the header the spec names, so a declared `X-Auth-Token` needs no override — don't reach for `AuthOverride` on the header name alone. `AuthOverride` (library mode) is for bearer-style credentials: the API wants `token <x>` or a raw token rather than the default `bearer <x>`, the spec puts the credential on `Authorization` but the API reads a different header, or the spec declares no scheme at all. The harness prints `config var "x" -> header "y"` per scheme; check it before deciding.
6. **Enum with `""` or `null` among its values** → those values are dropped from the generated SDK constants (they'd produce unnamed/invalid Go constants). Tell the user which values vanished; if a dropped value is meaningful, the property needs a plain string, not an enum.
7. **No `servers[0].url` (OAS3) / `host` (Swagger 2.0)** → `baseUrl` is empty; nothing fails until the first API call errors with `baseUrl is not set`. Supply `--base-url=` (parameterized), `Options.BaseURL` (library), or tell the user to `pulumi config set <pkg>:baseUrl`.
8. **Resource names look wrong** (`ApiV2Widgets`, `ExtrasGadgets`) → names come from the static collection-path segments, and only a leading `/api` is stripped. Rename with `ResourceOverride{Token}` → library mode.
9. **Huge spec** (hundreds of paths, e.g. Kubernetes, NetBox, Intercom) → discovery produces a resource per path group and SDK generation gets slow and noisy. Use `ExcludeTags` or per-resource `Skip` → library mode.

## Step 3a — Parameterized workflow

```bash
# 1. Install the binary as a Pulumi plugin (once per machine)
pulumi plugin install resource openapi-provider v0.1.0 \
  --server github://api.github.com/pierskarsenbarg/pulumi-openapi-provider

# 2. Generate a typed SDK for the spec, in the Pulumi project directory
pulumi package add openapi-provider 'https://api.example.com/openapi.json'
#    add --base-url=https://api.example.com when the spec declares no server
```

`pulumi package add` derives the package name from `info.title` slugified (`"Petstore API"` → `petstore-api`) and the version from `info.version` normalised to semver (`"v1.2"` → `1.2.0`, `"2024-05-01"` → `2024.0.0`, non-numeric → `1.0.0`). The package name is what every `pulumi config set` key and SDK import is based on, so state it explicitly to the user — it is rarely what they'd guess.

Then check the generated `sdk-<language>/` for the resources preflight predicted, and configure:

```bash
pulumi config set <pkg-name>:baseUrl https://api.example.com
pulumi config set <pkg-name>:<credential-var> <value> --secret
```

Building from a local spec file works too (`pulumi package add openapi-provider ./openapi.json`), which is the fastest way to iterate on a spec you're also editing.

## Step 3b — Library workflow

Minimal provider — build it as a binary named `pulumi-resource-<name>`:

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

With overrides and metadata, use the builder (it mirrors `infer.ProviderBuilder`, so `WithDescription`, `WithHomepage`, `WithLicense`, `WithResources`, `WithComponents`, `WithFunctions` all chain):

```go
builder, err := openapi.NewProviderBuilder("myprovider", "0.1.0", openapi.Options{
    SpecURL: "https://api.example.com/openapi.json",
    Overrides: map[string]openapi.ResourceOverride{
        "Widget": {IDField: "widget_id"},                     // create returns snake_case id
        "Team":   {UpdatePath: "/teams/{teamId}", UpdateMethod: "PATCH"},
        "Legacy": {Skip: true},
    },
    ExcludeTags: []string{"internal"},
})
if err != nil {
    log.Fatal(err)
}
if err := builder.
    WithDescription("Pulumi provider for Example API").
    WithResources(infer.Resource[*Office](&Office{})). // optional hand-written resources
    Run(context.Background()); err != nil {
    log.Fatal(err)
}
```

Full field table, hook signatures, and one focused example per override reason: `references/overrides-and-auth.md`. One constraint worth designing around up front: hand-written resources and hooks can't read the provider's config variables — there's no `WithConfig` on the builder — so anything added via `WithResources` needs its own endpoint and credential source.

Then schema and SDKs:

```bash
go build -o bin/pulumi-resource-myprovider .
pulumi package get-schema ./bin/pulumi-resource-myprovider > schema.json
pulumi package gen-sdk ./bin/pulumi-resource-myprovider --language all --out sdk
```

`openapi.GetSchema(name, version, opts)` returns the same schema JSON without starting the gRPC server — use it in CI, or as a second preflight signal.

In this repo specifically: `make build-examples`, `make schema`, `make gen-sdk` do the above for everything under `examples/`, and `make test` / `make lint` gate changes. Working examples to copy from: `examples/petstore` (Swagger 2.0, minimal), `examples/intercom` (OAS3, large), `examples/openapi-k8s` (custom `*http.Client` + wildcard `"*": {IDField: "metadata.name"}` override), and `integration-tests/code-provider/provider/main.go` (overrides + a `Check` hook + `WithResources` together).

## Step 4 — Verify, don't assume

A schema that generates is not a provider that works — ID extraction and auth only fail on the first real call. Run at least one `pulumi up` against the API (or the local API under `integration-tests/api`, started with `cd integration-tests && make run-api`), then `pulumi destroy`. `make test-code-provider` and `make test-parameterized-provider` exercise both modes end to end.

If something fails, `references/troubleshooting.md` maps symptoms → cause → fix.

## When this library is the wrong tool

Say so early rather than producing a provider with two resources out of forty:

- **RPC-style APIs** (`POST /createThing`, `POST /doAction`) — no `{id}` item paths means nothing is discoverable. Path-shape conventions can't help; a hand-written `infer` provider is the right answer.
- **Read-only or query APIs** with no `POST` collection endpoint — Pulumi resources need a create.
- **Auth this library can't send**: `apiKey in: query`, request signing (AWS SigV4-style), OAuth2 flows requiring a token exchange, or per-request signatures. A custom `*http.Client` with a signing `RoundTripper` covers some of these in library mode (see `examples/openapi-k8s`); a query-param key does not work at all today.
- **Non-JSON APIs** — request/response handling assumes `application/json`; `multipart/form-data` uploads and XML are not mapped.

Mixed cases are fine and common: build the spec-derived provider for the CRUD-shaped majority, and add the rest as hand-written resources via `WithResources`.

## Where the truth lives

The README, CLAUDE.md and these references were reconciled against the code and checked by running it, so they should agree today. `pkg/spec/resource.go`, `pkg/runtime/crud.go` and `pkg/config/config.go` are still authoritative: if prose and code disagree, the code wins, and the correction belongs in all three documents in the same commit — see `references/discovery-rules.md` §12. Prefer the preflight harness over any document when a specific spec is in front of you; it runs the real discovery functions and cannot go stale.
