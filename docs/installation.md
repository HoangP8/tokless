# Install tokless

Install and configure tokless for this user.

1. Detect operating system and installed supported agents.
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
   tokless --agents <comma-separated-agent-ids> --yes
   ```

   Valid IDs: `claude`, `opencode`, `codex`, `antigravity`, `copilot`, `droid`, `pi`.

5. Run `tokless doctor`. Report installed version, configured agents, and any warnings.

Do not modify agent configuration manually; let tokless own its managed sections.
