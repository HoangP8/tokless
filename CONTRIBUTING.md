# Contributing to tokless

tokless installs token-saving tools and wires them into AI coding agents. Commands iterate a central registry of agents and tools, so handlers contain no per-agent or per-tool branching.

## Layout

```
tokless/
├── cmd/tokless/        entrypoint: arg parsing + dispatch
├── scripts/            install.sh, install.ps1, build-release.sh
└── internal/
    ├── core/           registry types: AgentManifest, ToolManifest
    ├── agents/         one file per agent (claude, codex, opencode, cursor, ...)
    ├── tools/          one file per tool (rtk, codegraph, contextmode, projectmem, headroom, ...)
    ├── commands/       init, update, doctor, info, index, disable, selfupdate, proxy, ...
    ├── headroom/       headroom proxy support
    └── util/           config helpers (toml, jsonc, paths, exec, ...)
```

## Adding a tool or agent

| Add a... | Create | Register |
| :--- | :--- | :--- |
| **Tool** | a `ToolManifest` in `internal/tools/<name>.go` | one `core.RegisterTool(...)` line in `Register()` (`tools/contextmode.go`) |
| **Agent** | an `AgentManifest` in `internal/agents/<name>.go` | one `core.RegisterAgent(...)` line in `Register()` (`agents/codex.go`) |

Copy the nearest existing file as a template. A tool manifest carries its install step plus `WireFor`, `UnwireFor` and `VerifyFor` maps keyed by agent ID. Tools that only write instructions set `InstructionOnly`.

## Config-mutation rules

- **Idempotent**: writes must be byte-stable across runs. Never reorder or drop existing user keys.
- **Reversible**: unwire must remove exactly what wire wrote and leave user entries untouched.
- **JSON/JSONC**: parse, mutate and write through the ordered-map helpers in `util` to preserve key order.
- **TOML**: use the block helpers (upsert, remove, has) to edit sections in place.
- **Spawn commands**: use the helpers in `util/mcpspawn.go` and `util.ToklessPersistedAbs()` rather than hand-building commands.
- Every wired entry needs a matching verify step so `tokless doctor` can validate it.
- Cite the upstream tool's README URL for any config shape you write.

## Build & test

```bash
go build ./...                          # build
go vet ./...                            # static checks
go test ./...                           # unit + sandbox integration + idempotency
bash scripts/build-release.sh v0.2.0    # cross-compile all platform binaries into dist/release
```

Integration tests run under a temporary `HOME`, so they never touch your real agent configs. Some tests need `node`/`npm` on `PATH`. CI runs on Linux, macOS and Windows with Go 1.23.

`scripts/install.ps1` must stay ASCII-only so Windows PowerShell 5.1 parses it. CI enforces this.

## Releasing

CI runs build, test and the PowerShell checks on every push to `main` and every pull request. Pushing a `v*` tag builds every platform binary and publishes them to GitHub Releases. A tag containing `-` (for example `v1.2.0-rc1`) is published as a prerelease, and the installers ignore it.
