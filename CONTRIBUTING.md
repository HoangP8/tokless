# Contributing to tokless

tokless installs token-saving tools and wires them into AI coding agents. Commands work by iterating a central registry of agents and tools — no per-agent or per-tool branching in the handlers.

## Layout

```
tokless/
├── cmd/tokless/        entrypoint: arg parsing + dispatch
└── internal/
    ├── core/           registry + manifests
    ├── agents/         one file per agent (claude, opencode, codex)
    ├── tools/          one file per tool (rtk, caveman, codegraph, contextmode)
    ├── commands/       init, doctor, update, disable, selfupdate
    └── util/           config helpers (toml, jsonc, paths, exec, …)
```

## Adding a tool or agent

| Add a... | Create | Register |
| :--- | :--- | :--- |
| **Tool** | `internal/tools/<name>` defining a `ToolManifest` | one line in `Register()` (tools/contextmode) |
| **Agent** | `internal/agents/<name>` defining an `AgentManifest` | one line in `Register()` (agents/codex) |

Copy the nearest existing tool or agent file as a template — the manifests are self-documenting. A tool manifest carries its install step plus wire/unwire/verify maps keyed by agent.

## Config-mutation rules

- **Idempotent**: writes must be byte-stable across runs; never reorder existing user keys.
- **JSON/JSONC**: parse → mutate → stringify through the ordered-map helpers (preserves key order).
- **TOML**: use the block helpers (upsert / remove / has) to edit sections in place.
- **Spawn**: prefer a real binary on PATH, falling back to `npx --no-install`.
- Every wired entry needs a matching verify step so `tokless doctor` can validate it.
- Cite the upstream tool's README URL for any config shape you write.

## Build & test

```bash
go build ./...                          # build
go vet ./...                            # static checks
go test ./...                           # unit + sandbox integration + idempotency
bash scripts/build-release.sh v0.2.0    # cross-compile all platform binaries
```

The sandbox integration test wires all three agents under a temporary `HOME` and asserts idempotency.

## Releasing

CI runs vet + test + build on every push and pull request. Pushing a `v*` tag builds every platform binary and publishes them to GitHub Releases.
