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

### Installation (Manual)

<img src="assets/install.svg" width="100%" alt="install" />

Supported platforms: macOS/Linux x64 or arm64; Windows x64.

macOS / Linux:
```bash
curl -fsSL https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.sh | bash
```

Windows (PowerShell):
```powershell
irm https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.ps1 | iex
```

### Installation (Agent)

Let your coding agent install tokless, choose what to configure, and verify the result:

```text
https://raw.githubusercontent.com/HoangP8/tokless/main/docs/installation.md
```

### Agents

<div align="center">
  <table>
    <tr>
      <td align="center" width="140"><a href="https://github.com/anthropics/claude-code"><img src="assets/agents/claude.jpg" width="48" height="48" alt="Claude Code" /></a><br/><b>Claude Code</b><br/><sub><b style="color:#3fb950">✓ Supported</b></sub></td>
      <td align="center" width="140"><a href="https://github.com/anomalyco/opencode"><img src="assets/agents/opencode.png" width="48" height="48" alt="OpenCode" /></a><br/><b>OpenCode</b><br/><sub><b style="color:#3fb950">✓ Supported</b></sub></td>
      <td align="center" width="140"><a href="https://github.com/openai/codex"><img src="assets/agents/codex.jpg" width="48" height="48" alt="Codex" /></a><br/><b>Codex</b><br/><sub><b style="color:#3fb950">✓ Supported</b></sub></td>
      <td align="center" width="140"><a href="https://antigravity.google"><img src="assets/agents/antigravity.png" width="48" height="48" alt="Antigravity" /></a><br/><b>Antigravity</b><br/><sub><b style="color:#3fb950">✓ Supported</b></sub></td>
      <td align="center" width="140"><a href="https://github.com/github/copilot-cli"><img src="assets/agents/copilot.jpg" width="48" height="48" alt="GitHub Copilot" /></a><br/><b>GitHub Copilot</b><br/><sub><b style="color:#3fb950">✓ Supported</b></sub></td>
    </tr>
    <tr>
      <td align="center" width="140"><a href="https://factory.ai"><img src="assets/agents/factory.png" width="48" height="48" alt="Factory Droid" /></a><br/><b>Factory Droid</b><br/><sub><b style="color:#3fb950">✓ Supported</b></sub></td>
      <td align="center" width="140"><a href="https://pi.dev"><img src="assets/agents/pi.png" width="48" height="48" alt="Pi" /></a><br/><b>Pi</b><br/><sub><b style="color:#3fb950">✓ Supported</b></sub></td>
      <td align="center" width="140"><a href="https://cursor.com"><img src="assets/agents/cursor.jpg" width="48" height="48" alt="Cursor" /></a><br/><b>Cursor</b><br/><sub><b style="color:#d29922">In progress</b></sub></td>
      <td align="center" width="140"><a href="https://x.ai/cli"><picture><source media="(prefers-color-scheme: dark)" srcset="https://media.x.ai/v1/website/spacexai-symbol-white-transparent-0c31957f.png" /><source media="(prefers-color-scheme: light)" srcset="https://media.x.ai/v1/website/spacexai-symbol-black-transparent-6435cf42.png" /><img src="https://media.x.ai/v1/website/spacexai-symbol-black-transparent-6435cf42.png" width="48" height="48" alt="Grok Build" /></picture></a><br/><b>Grok Build</b><br/><sub><b style="color:#d29922">In progress</b></sub></td>
      <td align="center" width="140"><a href="https://kilo.ai/cli"><img src="https://raw.githubusercontent.com/junhoyeo/tokscale/main/.github/assets/client-kilocode.png" width="48" height="48" alt="Kilo Code" /></a><br/><b>Kilo Code</b><br/><sub><b style="color:#d29922">In progress</b></sub></td>
    </tr>
    <tr>
      <td align="center" width="140"><a href="https://omp.sh/"><img src="https://raw.githubusercontent.com/can1357/oh-my-pi/main/assets/icon.svg" width="48" height="48" alt="Oh My Pi" /></a><br/><b>Oh My Pi (OMP)</b><br/><sub><b style="color:#d29922">In progress</b></sub></td>
      <td align="center" width="140"><a href="https://cline.bot/"><img src="https://raw.githubusercontent.com/cline/cline/main/assets/icons/icon.png" width="48" height="48" alt="Cline" /></a><br/><b>Cline</b><br/><sub><b style="color:#d29922">In progress</b></sub></td>
      <td align="center" width="140">&nbsp;</td>
      <td align="center" width="140">&nbsp;</td>
      <td align="center" width="140">&nbsp;</td>
    </tr>
  </table>
</div>

Interactive install detects installed supported agents; choose one, some, or all:
```bash
tokless                              # interactive: pick agents
tokless --agents claude,opencode     # wire just these
```

## Included Tools

tokless combines focused agent rules with installed tools, exposing only the core MCP surface by default.

| Package | ⭐ | Purpose |
| :--- | :---: | :--- |
| [karpathy-skills](https://github.com/multica-ai/andrej-karpathy-skills) | ![](https://img.shields.io/github/stars/multica-ai/andrej-karpathy-skills?style=flat-square&label=) | Coding principles: think first, simplify, edit surgically, verify. |
| [caveman](https://github.com/JuliusBrussee/caveman) | ![](https://img.shields.io/github/stars/JuliusBrussee/caveman?style=flat-square&label=) | Terse response rules that preserve technical accuracy. |
| [ponytail](https://github.com/DietrichGebert/ponytail) | ![](https://img.shields.io/github/stars/DietrichGebert/ponytail?style=flat-square&label=) | Minimal-code rules: reuse first, avoid speculative work. |
| [rtk](https://github.com/rtk-ai/rtk) | ![](https://img.shields.io/github/stars/rtk-ai/rtk?style=flat-square&label=) | Filters command output before it reaches the agent. |
| [codegraph](https://github.com/colbymchenry/codegraph) | ![](https://img.shields.io/github/stars/colbymchenry/codegraph?style=flat-square&label=) | Indexed code graph for source, call paths, and impact. |
| [context-mode](https://github.com/mksglu/context-mode) | ![](https://img.shields.io/github/stars/mksglu/context-mode?style=flat-square&label=) | Sandboxed context tools with session memory and focused analysis. |

### Callable MCP surface

```text
Tools
├── CodeGraph · 1/1 tool
│   └── codegraph_explore
└── Context-Mode · 6/11 upstream tools
    ├── ctx_execute
    ├── ctx_batch_execute
    ├── ctx_execute_file
    ├── ctx_index
    ├── ctx_search
    └── ctx_fetch_and_index
```

## Configuration

tokless manages agent config and instruction blocks; not every item uses native config.

> **Instruction** means tokless-owned static rules. `tokless update` refreshes them.

| Tool | Claude | OpenCode | Codex | Antigravity | Copilot | Droid | Pi |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **rtk** | Hook + Allow | Plugin | Hook | Hook + Allow | Hook + Allow | Hook | Extension |
| **caveman** | Instruction | Instruction | Instruction | Instruction | Instruction | Instruction | Instruction |
| **ponytail** | Instruction | Instruction | Instruction | Instruction | Instruction | Instruction | Instruction |
| **codegraph** | MCP + Allow + Instruction | MCP + Instruction | MCP + Instruction | Hook + MCP + Instruction | Hook + MCP + Instruction | Hook + MCP + Instruction | MCP + Extension + Instruction |
| **context-mode** | MCP + Allow + Instruction | MCP + Instruction | Hook + MCP + Instruction | MCP + Instruction | MCP + Hook + Instruction | MCP + Instruction | MCP + Extension + Instruction |

## Usage

```
tokless              Install tools, then pick detected agents (safe to re-run)
tokless update       Update the tokless CLI, then show version diff and upgrade tools
tokless doctor       Show what's wired; warn about broken bits
tokless info         Show how tokless was installed, plus paths and config locations
tokless index        Build per-project codegraph indexes
tokless disable      Disable one or more agents
tokless uninstall    Remove everything tokless touched
tokless self-update  Update the tokless CLI itself
tokless --version    Print tokless version
tokless --help       Show all commands and flags
```

Flags:
```
--agents <list>   Subset: claude,opencode,codex,antigravity,copilot,droid,pi
--tools <list>    Subset: rtk,caveman,ponytail,codegraph,context-mode
--dry-run         Preview, no writes
--verbose         Every step
--yes             Skip confirmations
```

Restart agents after install so they pick up new config.
