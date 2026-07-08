# Integration tests

End-to-end integration tests live in [`integration-tests/`](integration-tests/). They spin up a shared local HTTP API and run two independent test suites against it — one for each usage mode.

## API

The test target is a [Hono](https://hono.dev) API running on [Bun](https://bun.sh), backed by SQLite via [Drizzle ORM](https://orm.drizzle.team). It exposes four resources covering both flat and nested (context-param) path patterns:

| Resource | Endpoints |
| --- | --- |
| User | `POST /users`, `GET/PATCH/DELETE /users/:userId` |
| Organisation | `POST /organisations`, `GET/PATCH/DELETE /organisations/:organisationId` |
| Team | `POST /organisations/:organisationId/teams`, `GET/PATCH/DELETE /organisations/:organisationId/teams/:teamId` |
| Member | `POST /organisations/:organisationId/teams/:teamId/members`, `GET/DELETE …/members/:memberId` |

The API serves its own OpenAPI spec at `GET /openapi`.

## Code-provider suite (`integration-tests/code-provider/`)

Tests library mode. The Go provider binary has the spec URL baked in at compile time. A TypeScript Pulumi program creates and destroys all four resource types to verify end-to-end CRUD.

## Parameterized-provider suite (`integration-tests/parameterized-provider/`)

Tests parameterized binary mode. The `pulumi-resource-openapi-provider` binary is installed as a Pulumi plugin, then `pulumi package add openapi-provider http://localhost:3000/openapi` fetches the spec at runtime, derives a package name (`integration-test-api`) from the API title, and generates a typed TypeScript SDK. The same CRUD Pulumi program then runs against the generated SDK.

## Running

```bash
# Terminal 1 — start the API (shared by both suites)
cd integration-tests && make run-api

# Terminal 2 — run both suites (code-provider, then parameterized-provider)
cd integration-tests && make test

# Or run each suite independently
cd integration-tests && make test-code-provider
cd integration-tests && make test-parameterized-provider
```
