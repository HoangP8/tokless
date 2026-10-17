# Install tokless

Install and configure tokless for this user.

1. Detect operating system and installed supported agents (16 harnesses; use the `opencode2` id for an isolated OpenCode v2 config).
2. Ask which detected agents to configure. Do not choose silently.
3. Run official installer:

   **macOS / Linux**

   ```bash
   curl -fsSL https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.sh | bash
   ```

   **Windows (PowerShell)**

   ```powershell
   irm https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.ps1 | iex
   ```

4. Configure selected agents explicitly:

   ```bash
   tokless --agents claude,opencode,codex
   ```

   Valid ids: `claude`, `opencode`, `opencode2`, `codex`, `antigravity`, `copilot`, `droid`, `grok`, `pi`, `omp`, `kilo`, `cline`, `cursor`, `amp`, `kimi`, `dsh`, `zcode`.

   Limit tools with `--tools rtk,caveman,ponytail,principles,codegraph,context-mode,headroom,projectmem`. Add `--dry-run` to preview writes without changing anything.

5. Run `tokless doctor`. Report installed version, configured agents, and any warnings.

## Optional Headroom Proxy

Headroom uses one local path:

```text
agent -> Headroom (127.0.0.1:8787) -> original provider
```

Start it for all supported agents, or select specific agents:

```bash
tokless proxy up
tokless proxy up --agents claude,opencode
```

Check routing:

```bash
tokless proxy status
```

Amp has no Headroom route; Cursor's proxy route is deprecated — it stays on native `api2.cursor.sh` OAuth.

Undo Tokless routing. A selected shutdown leaves shared Headroom running for
other agents; plain `proxy down` removes the full Tokless-managed set and stops
Headroom:

```bash
tokless proxy down --agents claude,opencode
tokless proxy down
```

Tokless changes native agent settings only. It does not edit `.zshrc`,
`.bashrc`, or other shell startup files. Existing OAuth and BYOK settings stay
with the agent/provider where supported. If a managed setting changes after
`proxy up`, `proxy down` refuses to overwrite it.

Do not modify agent configuration manually; let tokless own its managed sections.
