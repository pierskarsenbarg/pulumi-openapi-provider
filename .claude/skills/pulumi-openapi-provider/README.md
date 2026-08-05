# `pulumi-openapi-provider` skill

A [Claude Code skill](https://code.claude.com/docs/en/skills) that teaches an agent to build
Pulumi providers with this library — which mode to use, which endpoints in a given spec will
actually become resources, and which of them need an override before any code is generated.

The problem it solves: discovery drops path groups that don't fit the CRUD-by-path-shape
convention, silently. Without this skill an agent generates an SDK, hands it over, and the
missing resource surfaces later as a user's bug report. With it, the agent predicts the
result first and says what will be missing and why.

## Installing

**Working in this repository** — nothing to install. Claude Code discovers
`.claude/skills/*/SKILL.md` automatically, so the skill is available in any session started
here.

**Using it anywhere else** — copy the directory to your user-level skills folder:

```bash
cp -r .claude/skills/pulumi-openapi-provider ~/.claude/skills/
```

It is then available in every project. The one thing that needs adjusting outside this repo
is the preflight harness's module resolution — see below.

To share it with a team, either commit it to the consuming repository's `.claude/skills/`, or
package it as a `.skill` bundle with skill-creator's `package_skill.py`.

## What's in the bundle

| File | Purpose |
| ---- | ------- |
| `SKILL.md` | The instructions Claude loads when the skill triggers: mode decision tree, the preflight step, both workflows, and when this library is the wrong tool. |
| `references/discovery-rules.md` | Path grouping and CRUD detection as testable predicates, naming, tokens, ID resolution, type and enum mapping, plus two worked walkthroughs. |
| `references/overrides-and-auth.md` | `Options`, `ResourceOverride` fields and hooks, the wildcard key, how auth is derived and what is actually sent. |
| `references/troubleshooting.md` | Symptom → cause → fix, with error strings quoted from the code. |
| `scripts/preflight/` | A Go program that runs the library's real discovery against a spec and reports what a provider built from it would look like. |

Reference files are only read when a task needs them, which is why `SKILL.md` stays short.

## Running the preflight harness on its own

Useful outside an agent session too — it answers "what would a provider for this spec look
like?" in a couple of seconds, with no credentials and no `pulumi` CLI:

```bash
cd .claude/skills/pulumi-openapi-provider/scripts/preflight
go run . https://api.example.com/openapi.json          # or a local file path
go run . ./openapi.json mypackage                      # optional package name
```

It prints the discovered resources with their paths and ID fields, the spec paths no resource
claimed, update endpoints that discovery ignored, the auth schemes and generated config
variables, and the enum types.

The module has a `replace` pointing at the repository root, so it runs as-is from a checkout.
After copying the skill to `~/.claude/skills/`, repoint it:

```bash
go mod edit -replace github.com/pierskarsenbarg/pulumi-openapi-provider=/path/to/checkout
# ...or drop the replace and resolve the published module instead:
go mod edit -dropreplace github.com/pierskarsenbarg/pulumi-openapi-provider
go mod tidy
```

It is a separate Go module, so it does not affect `go build ./...`, `make test` or `make lint`
at the repository root.

## Changing the skill

Test prompts live in `../pulumi-openapi-provider-workspace/evals/evals.json`, with offline
spec fixtures alongside them. Each one exercises a trap the skill is supposed to catch
*before* code generation — a body-ID update, an ID field that won't extract, a non-standard
auth header, a dropped enum value — and carries assertions describing what a good answer
contains.

After editing the skill, re-run them by giving each prompt to a fresh agent that has the
skill available, and check the answers against the assertions. Run results are gitignored;
the prompts and fixtures are tracked. The skill-creator skill automates the run/grade/review
loop if you want the full treatment.

## Keeping it honest

The references were written from `pkg/spec/resource.go`, `pkg/runtime/crud.go` and
`pkg/config/config.go`, and verified by running discovery against fixtures. If discovery
behaviour changes, update the references in the same commit — a stale skill produces
confident wrong advice, which is worse than no skill. `references/discovery-rules.md` §12
covers this, and the preflight harness is the cheap guard: it calls the real functions, so it
can never disagree with the code.
