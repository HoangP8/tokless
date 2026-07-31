# Contributing to tokless

tokless installs token-saving tools and wires them into AI coding agents. Commands work by iterating a central registry of agents and tools — no per-agent or per-tool branching in the handlers.

## Layout

```
tokless/
├── cmd/tokless/        entrypoint: arg parsing + dispatch
└── internal/
    ├── core/           registry + manifests
    ├── agents/         one file per agent (7: claude, opencode, codex, antigravity, copilot, droid, pi)
    ├── tools/          one file per tool (8: rtk, principles, caveman, ponytail, codegraph, context-mode, headroom, projectmem)
    ├── commands/       init, doctor, update, index, disable, headroomproxy, selfupdate
    └── util/           config helpers (toml, jsonc, paths, exec, …)
```

## Adding a tool or agent

| Add a... | Create | Register |
| :--- | :--- | :--- |
| **Tool** | `internal/tools/<name>` defining a `ToolManifest` | one line in `Register()` (tools/contextmode) |
| **Agent** | `internal/agents/<name>` defining an `AgentManifest` | one line in `Register()` (agents/codex) |

Copy the nearest existing tool file — there are three shapes:

| Shape | Example | What it needs |
| :--- | :--- | :--- |
| **MCP server** | `headroom.go` | `mcpAgentMaps(id, ready)` for the three per-agent maps, a case in `util.SpawnForTool`, and a `testVersionFixture` entry |
| **Skill** (instructions only) | `caveman.go` | `skillAgentMaps(id)`, a `SkillSource`, a heading in `util.SectionsByOwner`, and a matching section in `agent_instructions.md` as the offline copy |
| **Tool with hooks** | `codegraph.go` | its own wire/unwire/verify — per-agent hook handling doesn't generalise |

Set `Channel` plus `Pkg`/`Repo`/`Bin` so version tracking works without touching anything else. PyPI tools also set `NeedsPython` and `MinPythonMinor`.

## Config-mutation rules

- **Idempotent**: writes must be byte-stable across runs; never reorder existing user keys.
- **JSON/JSONC**: parse → mutate → stringify through the ordered-map helpers (preserves key order).
- **TOML**: use the block helpers (upsert / remove / has) to edit sections in place.
- **Spawn**: prefer a real binary on PATH, falling back to `npx --no-install` (Node) or `uvx --from` (Python).
- Every wired entry needs a matching verify step so `tokless doctor` can validate it.
- Cite the upstream tool's README URL for any config shape you write.

## Build & test

```bash
go build ./...                          # build
go vet ./...                            # static checks
go test ./...                           # unit + sandbox integration + idempotency
bash scripts/build-release.sh v0.2.0    # cross-compile all platform binaries
```

The sandbox integration tests wire every agent under a temporary `HOME` and assert idempotency. Set `TOKLESS_TEST=1` for anything that would otherwise reach the network or the real config.

## Releasing

CI runs vet + test + build on every push and pull request. Pushing a `v*` tag builds every platform binary and publishes them to GitHub Releases.
