<div align="center">
  <img src="assets/logo.svg" width="180" alt="tokless" />

  **A unified pipeline for efficient and effective coding agents.**

  One tool, no config — works the moment it lands.

  [![version](https://img.shields.io/github/v/release/HoangP8/tokless?label=version)](https://github.com/HoangP8/tokless/releases)
  [![platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-blue)](https://github.com/HoangP8/tokless)
  [![license](https://img.shields.io/github/license/HoangP8/tokless)](https://github.com/HoangP8/tokless/blob/main/LICENSE)

  <br />

</div>

## Introduction

> *Many great packages make coding agents more **effective and efficient** — but discovering, installing, updating, and unifying them is painful, especially for non-technical users. The best tools exist; the **wiring is the real cost**.*

**tokless** — the lazy one-command solution.

<table>
<tr><td>✔️</td><td><b>Best packages, unified</b> — picks the most effective, efficient <a href="#tools">tools</a> and wires them without conflicts</td></tr>
<tr><td>✔️</td><td><b>One command, done</b> — pick your agent, restart, go</td></tr>
<tr><td>✔️</td><td><b>All platforms</b> — macOS, Linux, Windows</td></tr>
<tr><td>✔️</td><td><b>Zero config</b> — everything wired, no manual edits</td></tr>
<tr><td>✔️</td><td><b>Simple updates</b> — <code>tokless update</code> upgrades everything in one shot</td></tr>
<tr><td>✔️</td><td><b>Non-tech friendly</b> — under 30 seconds, anyone can do it</td></tr>
</table>

### Installation

<img src="assets/install.svg" width="100%" alt="install" />

macOS / Linux:
```bash
curl -fsSL https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.sh | bash
```

Windows (PowerShell):
```powershell
irm https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.ps1 | iex
```

### Supported Agents

<div align="center">
  <table>
    <tr>
      <td align="center" width="140">
        <img src="assets/agents/claude.jpg" width="56" alt="Claude Code" /><br/>
        <b>Claude Code</b><br/>
        <sub><b style="color:#3fb950">✓ Done</b></sub>
      </td>
      <td align="center" width="140">
        <img src="assets/agents/opencode.png" width="56" alt="OpenCode" /><br/>
        <b>OpenCode</b><br/>
        <sub><b style="color:#3fb950">✓ Done</b></sub>
      </td>
      <td align="center" width="140">
        <img src="assets/agents/codex.jpg" width="56" alt="Codex" /><br/>
        <b>Codex</b><br/>
        <sub><b style="color:#3fb950">✓ Done</b></sub>
      </td>
      <td align="center" width="140">
        <img src="assets/agents/antigravity.png" width="56" alt="Antigravity" /><br/>
        <b>Antigravity</b><br/>
        <sub><b style="color:#3fb950">✓ Done</b></sub>
      </td>
    </tr>
    <tr>
      <td align="center" width="140">
        <img src="assets/agents/pi.png" width="56" alt="Pi" /><br/>
        <span style="color:#8b949e"><b>Pi</b></span><br/>
        <sub><b style="color:#d29922">In progress</b></sub>
      </td>
      <td align="center" width="140">
        <img src="assets/agents/cursor.jpg" width="56" alt="Cursor" /><br/>
        <span style="color:#8b949e"><b>Cursor</b></span><br/>
        <sub><b style="color:#d29922">In progress</b></sub>
      </td>
      <td align="center" width="140">
        <img src="assets/agents/factory.png" width="56" alt="Factory Droid" /><br/>
        <span style="color:#8b949e"><b>Factory Droid CLI</b></span><br/>
        <sub><b style="color:#d29922">In progress</b></sub>
      </td>
      <td align="center" width="140">
        <img src="assets/agents/copilot.jpg" width="56" alt="GitHub Copilot" /><br/>
        <span style="color:#8b949e"><b>GitHub Copilot</b></span><br/>
        <sub><b style="color:#d29922">In progress</b></sub>
      </td>
    </tr>
  </table>
</div>

Pick one, some, or all:
```bash
tokless                              # interactive: pick agents
tokless --agents claude,opencode     # wire just these
tokless --agents claude,opencode,codex,antigravity  # all
```

### Tools

| Tool | ⭐ | What it does |
| :--- | :---: | :--- |
| [karpathy-skills](https://github.com/multica-ai/andrej-karpathy-skills) | ![](https://img.shields.io/github/stars/multica-ai/andrej-karpathy-skills?style=flat-square&label=) | Distilled meta-rules from Karpathy's LLM-coding post — think before coding, simplicity first, surgical changes, goal-driven. Drops overbuild and wrong-assumption failures. |
| [caveman](https://github.com/JuliusBrussee/caveman) | ![](https://img.shields.io/github/stars/JuliusBrussee/caveman?style=flat-square&label=) | Skill/plugin forcing terse caveman-speak across 30+ agents — 65% output token cut, technical content untouched. |
| [ponytail](https://github.com/DietrichGebert/ponytail) | ![](https://img.shields.io/github/stars/DietrichGebert/ponytail?style=flat-square&label=) | Skill embedding a lazy senior dev — minimum-code, stdlib-first, no speculative features across 16 agents. |
| [rtk](https://github.com/rtk-ai/rtk) | ![](https://img.shields.io/github/stars/rtk-ai/rtk?style=flat-square&label=) | CLI proxy filtering/compressing command output before it hits the LLM; 100+ commands, single Rust binary, <10ms overhead. |
| [codegraph](https://github.com/colbymchenry/codegraph) | ![](https://img.shields.io/github/stars/colbymchenry/codegraph?style=flat-square&label=) | Pre-indexed SQLite code knowledge graph — auto-syncs on file change, returns verbatim source + call paths via 1 MCP tool across 8+ agents. |
| [context-mode](https://github.com/mksglu/context-mode) | ![](https://img.shields.io/github/stars/mksglu/context-mode?style=flat-square&label=) | MCP server for 17 clients — sandboxes tool output, persists session memory, enforces think-in-code. |

## Configuration

Each tool is wired into each agent through the agent's native config system — MCP servers, plugin registries, hooks, instruction files (`CLAUDE.md` / `AGENTS.md` / `GEMINI.md`).

| Tool | Claude | OpenCode | Codex | Antigravity |
| :--- | :--- | :--- | :--- | :--- |
| **rtk** | PreToolUse + Allow | Plugin | PreToolUse + PermissionRequest + Trust + Rules | PreToolUse + Allow |
| **caveman** | Plugin + CLAUDE.md | Plugin + AGENTS.md | Skills + AGENTS.md | Skills + GEMINI.md |
| **ponytail** | Plugin + CLAUDE.md | Plugin + AGENTS.md | Marketplace + AGENTS.md | Extension + GEMINI.md |
| **codegraph** | MCP + Allow + CLAUDE.md | MCP + AGENTS.md | MCP + AGENTS.md | PostToolUse + PreInvocation + MCP + Allow + GEMINI.md |
| **context-mode** | MCP + Allow + CLAUDE.md | Plugin + AGENTS.md | PreToolUse + MCP + AGENTS.md | MCP + Allow + GEMINI.md |

## Usage

```
tokless              Install + wire everything (default; safe to re-run)
tokless update       Show version diff and upgrade tools
tokless doctor       Show what's wired; warn about broken bits
tokless index        Build per-project codegraph indexes
tokless disable      Disable one or more agents
tokless uninstall    Remove everything tokless touched
tokless self-update  Update the tokless CLI itself
tokless --version    Print tokless version
tokless --help       Show all commands and flags
```

Flags:
```
--agents <list>   Subset: claude,opencode,codex,antigravity
--tools <list>    Subset: rtk,caveman,ponytail,codegraph,context-mode
--dry-run         Preview, no writes
--verbose         Every step
--yes             Skip confirmations
```

Restart agents after install so they pick up new config.
