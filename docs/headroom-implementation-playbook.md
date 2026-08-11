# Headroom Implementation Playbook

## Scope

Add Headroom as optional Tokless MCP tool. Headroom gives models two explicit
operations:

- `headroom_compress`: compress supplied text into smaller lossy text.
- `headroom_retrieve`: recover retained original text by hash during Headroom's
  local retention window.

This is MCP-first, on-demand compression. Registering server does not cause
model tool output, conversation context, or arbitrary MCP traffic to be
compressed automatically. Do not describe it as transparent compression.

Do not implement Headroom's separate proxy mode in this change. Proxy mode has
different behavior and lifecycle from its direct MCP server.

Success means selected agents receive one managed `headroom` MCP registration,
only two intended tools are visible/callable through Tokless's wrapper, the
installation is isolated from user Python, configure/update/remove are
idempotent, and unsupported native platforms receive useful diagnosis.

## Design Decisions

| Area | Decision |
| --- | --- |
| Tool ID | `headroom` |
| Upstream command | `headroom mcp serve` |
| Public MCP tools | `headroom_compress`, `headroom_retrieve` |
| Hidden upstream tools | `headroom_stats`, plus opt-in `headroom_read` |
| Installation | `uv` managed tool, pinned to managed Python 3.13 |
| Python isolation | Dedicated Tokless directories, never user site-packages or project venv |
| Agent wiring | Reuse standard Tokless MCP wiring for every supported agent |
| Tool visibility | Enforce at JSON-RPC proxy, not only in agent prompt/config |
| Compression invocation | Explicit model tool call, reinforced by managed instructions where supported |
| Compression scope | Large, self-contained text; never credentials, private keys, tokens, or opaque binaries |
| Retrieval | Best effort only; original is local and expires after Headroom TTL, documented as 1 hour |

`headroom_compress` is lossy. Model must retain enough task context to decide
whether a compressed result is suitable. `headroom_retrieve` is not durable
storage and must not be used as a replacement for a source file, database, or
user-visible artifact.

## Phase 1: Manifest And Process Contract

### Files

- Add `internal/tools/headroom.go`.
- Register it with existing tool registry in `internal/tools`.
- Add focused tests alongside existing tool lifecycle tests.

### Manifest

Create a `core.ToolManifest` matching existing CodeGraph and Context Mode
shape:

- `ID: "headroom"`
- user-facing label and description that say "on-demand MCP compression"
- install/check/update functions
- `WireFor` and `UnwireFor` entries for every agent currently supported by
  generic MCP configuration
- no ownership of user files beyond Tokless-managed MCP entries and Tokless
  instruction blocks

Tool command must be represented through existing `util.McpSpawn` machinery,
not duplicated per agent. The unwrapped child process is:

```text
headroom mcp serve
```

Tokless's registered command must run through `tokless run-mcp` so the bounded
MCP proxy controls the exposed surface. Keep child-command construction in one
place, likely `internal/util/mcpspawn.go`.

### Expected Process Shape

For every generated agent configuration, semantic process shape is:

```text
tokless run-mcp --tool headroom <managed-headroom-executable> mcp serve
```

Use existing exact command serialization conventions for JSON, TOML, YAML, and
agent-specific configuration. Do not require `headroom` to be on interactive
shell `PATH`; resolve and register Tokless-managed executable path.

### Acceptance Checks

- Tool registry includes `headroom` exactly once.
- Spawn resolver produces wrapper command plus child `mcp serve` arguments.
- A direct Headroom process is never written into agent configuration.
- Existing CodeGraph and Context Mode process shapes do not change.

## Phase 2: Bounded Generic MCP Proxy

### Problem

`internal/commands/mcp_proxy.go` currently implements Context Mode-specific
filtering. Headroom upstream exposes more than Tokless promises:

- `headroom_compress`
- `headroom_retrieve`
- `headroom_stats`
- `headroom_read` only when `HEADROOM_MCP_READ` enables it

Prompt text and agent configuration cannot reliably hide tools from every MCP
host. Enforce least privilege in Tokless's JSON-RPC wrapper.

### Change

Generalize wrapper configuration from a Context Mode special case to a
tool-to-allowed-tools policy. Keep it data-only and small. For example, central
policy shape may map:

```text
context-mode -> existing Context Mode allowlist
headroom     -> [headroom_compress, headroom_retrieve]
```

Do not infer access from tool-name prefixes. Unknown tool IDs have no filtered
policy and retain existing behavior unless wrapper contract explicitly requires
a policy. Known bounded tools must have a non-empty explicit allowlist.

`run-mcp` needs tool identity before it launches child process. Add a
backward-compatible explicit `--tool <id>` argument to generated wrapped
commands. Parse this before child command, consistent with current `--agent`,
`--workspace`, and `--context-mode` flags. Do not introduce a `--` separator
unless parser and every writer deliberately support it. Existing old wrapper
invocations must retain their current behavior until all writers have migrated.

Proxy behavior for bounded tools:

1. Forward initialization and unrelated JSON-RPC methods unchanged.
2. On `tools/list`, retain upstream payload shape but remove entries whose
   `name` is not allowed.
3. On `tools/call`, reject a disallowed tool before forwarding upstream.
4. Return valid JSON-RPC error with stable machine-readable message; never
   execute hidden tool as fallback.
5. Preserve request IDs, JSON decoding limits, cancellation, stderr handling,
   and child process lifecycle.

Do not edit tool schemas. Passing `headroom_stats` through `tools/list` then
hoping models ignore it is a failure.

### Tests

Extend `internal/commands/mcp_proxy_test.go` with a fake child MCP server:

- Headroom list forwards only `headroom_compress` and `headroom_retrieve`.
- `headroom_stats` and `headroom_read` are absent even when child advertises
  both.
- allowed call forwards unchanged and returns child result.
- disallowed call returns error and fake child observes no call.
- existing Context Mode filtering remains byte-for-byte behaviorally covered.
- unbounded or legacy invocation retains current pass-through behavior.

Extend `internal/commands/runmcp_test.go`:

- `--tool headroom` parses before child command.
- child command and arguments remain unchanged.
- malformed/missing `--tool` value fails before process launch.

## Phase 3: Managed Installation

### Target Layout

Use Tokless-owned directories under existing Tokless application-data root. Add
one small helper for paths rather than scattering environment variables:

```text
<tokless-data>/headroom/uv/uv[.exe]
<tokless-data>/headroom/tools/
<tokless-data>/headroom/bin/headroom[.exe]
<tokless-data>/headroom/python/
```

Set command environment only for Tokless's `uv` execution:

```text
UV_TOOL_DIR=<tokless-data>/headroom/tools
UV_TOOL_BIN_DIR=<tokless-data>/headroom/bin
UV_PYTHON_INSTALL_DIR=<tokless-data>/headroom/python
UV_MANAGED_PYTHON=1
```

This prevents writes to `~/.local`, user `uv` tool directories, user Python,
or project virtual environments. If directory naming must match existing
Tokless cache/install conventions, follow those conventions instead of adding
a second root.

### Bootstrap And Install

Install is best effort, with clear stage-specific reporting:

1. Resolve Tokless-managed `uv`; if present, use it.
2. Otherwise locate system `uv` only if it can run and Tokless can still direct
   all managed state into Tokless-owned paths.
3. Otherwise download the official standalone `uv` installer to temporary
   location, set `UV_INSTALL_DIR` to Tokless-owned bin location and
   `UV_NO_MODIFY_PATH=1`, then run platform-appropriate official installer.
4. Resolve installed `uv` by absolute path, never shell profile mutation.
5. Run `uv tool install --python 3.13 'headroom-ai[mcp]'` with managed-state
   environment. On update, run equivalent `uv tool install --upgrade` command.
6. Verify managed `headroom` executable exists and `headroom mcp serve` can
   start sufficiently to prove entry point is valid. Kill probe cleanly; do not
   leave a running server.

Never run `pip install`, mutate global Python, add a project dependency, or
install shell startup snippets.

Avoid hard-coding an upstream package version unless Tokless version policy
already pins external MCP dependencies. Version policy must be shared with
existing tool update/status behavior.

### Platform Diagnosis

Headroom supports Python `>=3.10`; Tokless requests Python 3.13 because `uv`
can manage it independently. Published wheels cover Linux `x86_64`, Linux
`aarch64`, and Apple Silicon macOS for Python 3.10 through 3.13.

On Windows and Intel macOS, installation may fall back to source build of its
Rust extension. Detect these platform/architecture combinations before install
or classify build failure afterward. Report that a Rust/native toolchain may be
required, include captured concise build tail in verbose mode, and give manual
recovery command. Do not claim Tokless can guarantee automatic install there.

Installation failures must:

- identify failed stage: `uv bootstrap`, managed Python, package install, or
  executable verification;
- preserve original agent configuration by wiring only after install succeeds;
- not delete a previously working managed install on failed upgrade;
- return actionable non-zero result;
- suggest the exact generated command under verbose mode, with no secrets.

### Checks And Updates

`Check` should distinguish:

- missing `uv` or executable;
- managed executable present but unusable;
- executable working;
- unsupported/native-build-risk platform, as warning not a false success.

Use existing `internal/util/versions.go` conventions for installed/latest
status. Do not implement a second network/version client if `uv` package
metadata or current Tokless package-check path already answers it.

### Tests

Unit-test path and command builders without network/process dependency:

- managed paths are inside Tokless root;
- all required `UV_*` values point inside that root;
- install versus upgrade uses intended flags;
- no builder yields `pip`, user-site, global Python, or shell-profile command;
- generated install invocation requests Python 3.13 and `headroom-ai[mcp]`;
- Windows extension is handled when platform helpers require it;
- native-build-prone platform errors produce remediation text.

Use existing injectable runner seams to test staged errors. Do not add a live
network install test to normal suite.

## Phase 4: Generic Agent Wiring

### Required Refactor

Audit each per-agent MCP writer, matcher, verifier, and remover. Several
currently recognize `codegraph` specially and treat every other MCP as
`context-mode`. Replace binary assumptions with a small shared MCP definition
or explicit switch that recognizes `headroom`.

Required affected patterns include:

- `internal/agents/claude.go`: non-CodeGraph wrapped command resolution.
- `internal/agents/kilo.go`: expected wrapped command matching.
- `internal/agents/cline.go`: expected wrapped command matching.
- `internal/agents/omp.go`: non-CodeGraph matcher assumption.
- `internal/agents/droid.go`: allowed tool-name mapping.

Use exact values for Headroom:

```text
server name: headroom
visible tools: headroom_compress, headroom_retrieve
```

Do not broaden Context Mode permissions or alter CodeGraph's existing
configuration as part of this refactor.

### Ownership And Idempotence

Reuse `WriteOwner`, tool blocks, and existing config ownership rules:

- Configure only Tokless-owned `headroom` MCP entries.
- Preserve unrelated user MCP servers, user command overrides, comments, key
  order, and agent settings as existing writers do.
- Re-running configure converges to one correct Headroom entry.
- Update repairs stale Tokless-generated Headroom command shapes.
- Disable/removal removes only Headroom registration and Headroom-owned
  instructions; no package removal during selective tool disable.
- Full uninstall may remove Tokless-owned Headroom managed directories only
  after all agents and all tools are selected, matching current uninstall
  safety contract.

Add Headroom package cleanup to `runPurge` only if package is demonstrably
installed by Tokless in a location current purge contract owns. Since Headroom
is a private `uv` tool rather than npm global, its cleanup must be a separate,
safe managed-directory removal, never a broad `uv tool uninstall` against user
state.

### Test Matrix

For every supported agent:

- configure adds expected Headroom registration once;
- verification recognizes managed command;
- user-owned unrelated MCP registration survives;
- second configure produces no duplicate/drift;
- selective `--tools headroom` removal removes Headroom only;
- full disable/uninstall cleans Headroom owner state according to contract;
- dry-run reports intended operation without filesystem mutation.

Use table-driven fixtures where agent formats share a test helper. Keep
agent-specific fixture tests where formats differ.

## Phase 5: Model Guidance And Hooks

### Guidance

Add short managed instructions only where Tokless already writes agent
instructions. Keep them operational:

```text
For large, self-contained text that must remain in current task, call
headroom_compress explicitly. It is lossy. Call headroom_retrieve with returned
hash only when original retained text is needed soon; retrieval is local and
expires. Never send credentials, tokens, private keys, or sensitive content.
```

Do not add generic prompt bloat to agents that do not support Tokless-managed
instruction ownership. Do not claim this causes tool use; models retain choice.

### Hooks

No automatic compression hook in first implementation. Existing hooks cannot
portably intercept and replace arbitrary agent/MCP tool output before context
ingestion across all supported hosts. A hook that merely prompts model is
guidance, not automatic compression.

If future work adds host-specific assistance, it must be opt-in and document:

- supported host and exact event boundary;
- byte/token threshold;
- text-only eligibility;
- exclusions for secrets and user content;
- failure behavior that passes original content untouched;
- how compressed content and retrieval hashes appear to model;
- independent integration tests on that host.

No lifecycle or proxy changes should depend on a hook existing.

## Phase 6: Doctor, Status, And User Output

Integrate with existing doctor and init/update summaries:

- state whether Headroom is installed and which agents are wired;
- distinguish binary health from agent configuration health;
- report wrapped direct-MCP behavior as on-demand;
- report native toolchain warning when applicable;
- show non-sensitive manual recovery command when install fails;
- treat temporary retrieval cache absence as normal, not installation failure.

Do not print Headroom source text, retrieval hashes from prior sessions, or
environment values that could contain credentials.

## Implementation Order

1. Add manifest, managed-path, and command-builder unit tests.
   Verify: registry/command tests pass without network access.
2. Generalize `run-mcp` tool policy and add fake-server proxy tests.
   Verify: only two Headroom tools listed/callable; Context Mode remains intact.
3. Generalize agent command matching/wiring and add fixture tests.
   Verify: each supported agent configures, verifies, reconfigures, and removes
   Headroom without modifying unrelated configuration.
4. Implement managed `uv` bootstrap/install/check/update/uninstall cleanup.
   Verify: mocked runner tests cover every stage and preserve working install on
   failed upgrade.
5. Add managed instruction text and doctor/status output.
   Verify: snapshots/assertions state explicit, lossy, temporary semantics.
6. Run complete verification and repair failures before final review.

## Required Verification Loop

Run these after implementation. Use focused commands first, then full suite.

```bash
go test ./internal/commands ./internal/tools ./internal/agents ./internal/util
go test ./...
go vet ./...
rtk git diff --check
```

If repository standard has a different canonical test/lint command, use it in
addition to these. Test failures must be triaged to root cause, fixed, and
rerun at focused scope before full suite rerun. Do not mark implementation
complete with skipped platform/build failure tests unless environment limitation
is stated and unit coverage proves expected diagnosis path.

Before merge/release, manually inspect generated configs for at least one JSON,
TOML, and YAML agent format, then run:

```bash
tokless --agents <fixture-or-test-agent> --tools headroom --yes
tokless doctor
tokless disable --agents <fixture-or-test-agent> --tools headroom --yes
```

Run manual commands only against disposable test homes/config roots supported by
existing test harness. Never modify developer's live agent configuration while
validating.

## Non-Goals

- Automatic compression of all model context or MCP responses.
- Headroom proxy-mode installation or routing.
- Durable retrieval, cross-machine retrieval, or a Tokless retrieval store.
- Headroom model-provider configuration or credential management.
- Global Python, `pip`, npm, or shell-profile changes.
- Guaranteeing native builds on Windows or Intel macOS without prerequisite
  toolchains.
- Exposing `headroom_stats` or `headroom_read` through Tokless.
