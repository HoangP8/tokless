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

> *Many packages help coding agents work more effectively and efficiently. **The best tools already exist; wiring them well is the real work.** Tokless brings together [specialized tools](#tools), each with a distinct purpose, and manages them with minimal setup.*

<table>
  <tr><td>✅</td><td><b>Clear purpose</b> — best tool for each job, wired without conflict</td></tr>
  <tr><td>✅</td><td><b>No overload</b> — minimal MCP tools with clear, unified instructions</td></tr>
  <tr><td>✅</td><td><b>Zero config</b> — works as soon as installed, in under 30 seconds</td></tr>
  <tr><td>✅</td><td><b>One place</b> — install, update, and manage all tools across every configured agent</td></tr>
</table>

<p align="center"><b>AGENTS</b></p>

<div align="center">
  <table>
    <tr>
      <td align="center" width="140"><a href="https://github.com/anthropics/claude-code"><img src="assets/agents/claude.jpg" width="48" height="48" alt="Claude Code" /></a><br/><b>Claude Code</b><br/><img src="https://img.shields.io/badge/-%E2%9C%93%20Supported-2ea44f" alt="✓ Supported" /></td>
      <td align="center" width="140"><a href="https://github.com/anomalyco/opencode"><img src="assets/agents/opencode.png" width="48" height="48" alt="OpenCode" /></a><br/><b>OpenCode</b><br/><img src="https://img.shields.io/badge/-%E2%9C%93%20Supported-2ea44f" alt="✓ Supported" /></td>
      <td align="center" width="140"><a href="https://github.com/openai/codex"><img src="assets/agents/codex.jpg" width="48" height="48" alt="Codex" /></a><br/><b>Codex</b><br/><img src="https://img.shields.io/badge/-%E2%9C%93%20Supported-2ea44f" alt="✓ Supported" /></td>
      <td align="center" width="140"><a href="https://antigravity.google"><img src="assets/agents/antigravity.png" width="48" height="48" alt="Antigravity" /></a><br/><b>Antigravity</b><br/><img src="https://img.shields.io/badge/-%E2%9C%93%20Supported-2ea44f" alt="✓ Supported" /></td>
      <td align="center" width="140"><a href="https://github.com/github/copilot-cli"><img src="assets/agents/copilot.jpg" width="48" height="48" alt="GitHub Copilot" /></a><br/><b>GitHub Copilot</b><br/><img src="https://img.shields.io/badge/-%E2%9C%93%20Supported-2ea44f" alt="✓ Supported" /></td>
    </tr>
    <tr>
      <td align="center" width="140"><a href="https://factory.ai"><img src="assets/agents/factory.png" width="48" height="48" alt="Factory Droid" /></a><br/><b>Factory Droid</b><br/><img src="https://img.shields.io/badge/-%E2%9C%93%20Supported-2ea44f" alt="✓ Supported" /></td>
      <td align="center" width="140"><a href="https://pi.dev"><img src="assets/agents/pi.png" width="48" height="48" alt="Pi" /></a><br/><b>Pi</b><br/><img src="https://img.shields.io/badge/-%E2%9C%93%20Supported-2ea44f" alt="✓ Supported" /></td>
      <td align="center" width="140"><a href="https://cursor.com"><img src="assets/agents/cursor.jpg" width="36" height="36" alt="Cursor" /></a><br/>Cursor<br/><img src="https://img.shields.io/badge/-In%20progress-8c959f" alt="In progress" /></td>
      <td align="center" width="140"><a href="https://x.ai/cli"><picture><source media="(prefers-color-scheme: dark)" srcset="https://media.x.ai/v1/website/spacexai-symbol-white-transparent-0c31957f.png" /><source media="(prefers-color-scheme: light)" srcset="https://media.x.ai/v1/website/spacexai-symbol-black-transparent-6435cf42.png" /><img src="https://media.x.ai/v1/website/spacexai-symbol-black-transparent-6435cf42.png" width="36" height="36" alt="Grok Build" /></picture></a><br/>Grok Build<br/><img src="https://img.shields.io/badge/-In%20progress-8c959f" alt="In progress" /></td>
      <td align="center" width="140"><a href="https://kilo.ai/cli"><img src="https://raw.githubusercontent.com/junhoyeo/tokscale/main/.github/assets/client-kilocode.png" width="36" height="36" alt="Kilo Code" /></a><br/>Kilo Code<br/><img src="https://img.shields.io/badge/-In%20progress-8c959f" alt="In progress" /></td>
    </tr>
    <tr>
      <td align="center" width="140"><a href="https://omp.sh/"><img src="https://raw.githubusercontent.com/can1357/oh-my-pi/main/assets/icon.svg" width="36" height="36" alt="Oh My Pi" /></a><br/>Oh My Pi<br/><img src="https://img.shields.io/badge/-In%20progress-8c959f" alt="In progress" /></td>
      <td align="center" width="140"><a href="https://cline.bot/"><img src="https://raw.githubusercontent.com/cline/cline/main/assets/icons/icon.png" width="36" height="36" alt="Cline" /></a><br/>Cline<br/><img src="https://img.shields.io/badge/-In%20progress-8c959f" alt="In progress" /></td>
      <td align="center" width="140">&nbsp;</td>
      <td align="center" width="140">&nbsp;</td>
      <td align="center" width="140">&nbsp;</td>
    </tr>
  </table>
</div>

## Installation

### Manual

<img src="assets/install.svg" width="100%" alt="install" />

**macOS / Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.sh | bash
```

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.ps1 | iex
```

Nothing to install first. Node and Python are only needed by some tools, and
tokless installs whatever is missing (Python arrives via `uv`, which brings its
own).

Interactive install detects installed supported agents; choose one, some, or all:

```bash
tokless                              # interactive: pick agents
tokless --agents claude,opencode     # wire just these
```

### Copy this for your agent

```text
https://raw.githubusercontent.com/HoangP8/tokless/main/docs/installation.md
```

## Tools

Popular packages with distinct roles, wired without conflicts.

| Package | ⭐ | Purpose |
| :--- | :---: | :--- |
| [karpathy-skills](https://github.com/multica-ai/andrej-karpathy-skills) | ![](https://img.shields.io/github/stars/multica-ai/andrej-karpathy-skills?style=flat-square&label=) | Coding principles: think first, simplify, edit surgically, verify. |
| [caveman](https://github.com/JuliusBrussee/caveman) | ![](https://img.shields.io/github/stars/JuliusBrussee/caveman?style=flat-square&label=) | Terse response rules that preserve technical accuracy. |
| [ponytail](https://github.com/DietrichGebert/ponytail) | ![](https://img.shields.io/github/stars/DietrichGebert/ponytail?style=flat-square&label=) | Minimal-code rules: reuse first, avoid speculative work. |
| [rtk](https://github.com/rtk-ai/rtk) | ![](https://img.shields.io/github/stars/rtk-ai/rtk?style=flat-square&label=) | Filters command output before it reaches the agent. |
| [codegraph](https://github.com/colbymchenry/codegraph) | ![](https://img.shields.io/github/stars/colbymchenry/codegraph?style=flat-square&label=) | Indexed code graph for source, call paths, and impact. |
| [context-mode](https://github.com/mksglu/context-mode) | ![](https://img.shields.io/github/stars/mksglu/context-mode?style=flat-square&label=) | Sandboxed context tools with session memory and focused analysis. |
| [headroom](https://github.com/headroomlabs-ai/headroom) | ![](https://img.shields.io/github/stars/headroomlabs-ai/headroom?style=flat-square&label=) | Compresses payloads that have to enter context anyway. |
| [projectmem](https://github.com/riponcm/projectmem) | ![](https://img.shields.io/github/stars/riponcm/projectmem?style=flat-square&label=) | Local memory of past issues, fixes, and decisions. |

```text
MCP Tools
├── CodeGraph · 1/1
│   └── codegraph_explore
├── Context Mode · 6/11
│   ├── ctx_execute
│   ├── ctx_batch_execute
│   ├── ctx_execute_file
│   ├── ctx_index
│   ├── ctx_search
│   └── ctx_fetch_and_index
├── Headroom · 3/3
│   ├── headroom_compress
│   ├── headroom_retrieve
│   └── headroom_stats
└── ProjectMem · 15/15
    ├── get_context · precheck_file · get_global_gotchas
    ├── get_instructions · get_summary · get_project_map
    ├── get_plan · get_issue · search_events · get_score
    └── log_issue · record_attempt · record_fix · add_decision · add_note
```

Skill prose (principles, caveman, ponytail) is synced from each upstream repo and
versioned, so `tokless update` picks up changes there too. The copy bundled in the
binary is the offline fallback.

## Configuration

How each tool connects to each agent:

> **Instruction** means Tokless-managed static rules refreshed by `tokless update`.

<table width="1500">
  <thead>
    <tr>
      <th width="130" nowrap>Tool</th>
      <th width="185" nowrap>Claude</th>
      <th width="185" nowrap>OpenCode</th>
      <th width="185" nowrap>Codex</th>
      <th width="220" nowrap>Antigravity</th>
      <th width="220" nowrap>Copilot</th>
      <th width="220" nowrap>Droid</th>
      <th width="225" nowrap>Pi</th>
    </tr>
  </thead>
  <tbody>
    <tr><td nowrap><b>rtk</b></td><td nowrap>Hook + Allow</td><td nowrap>Plugin</td><td nowrap>Hook</td><td nowrap>Hook + Allow</td><td nowrap>Hook + Allow</td><td nowrap>Hook</td><td nowrap>Extension</td></tr>
    <tr><td nowrap><b>caveman</b></td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td></tr>
    <tr><td nowrap><b>ponytail</b></td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td></tr>
    <tr><td nowrap><b>codegraph</b></td><td nowrap>MCP + Allow + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>Hook + MCP + Instruction</td><td nowrap>Hook + MCP + Instruction</td><td nowrap>Hook + MCP + Instruction</td><td nowrap>MCP + Extension + Instruction</td></tr>
    <tr><td nowrap><b>context-mode</b></td><td nowrap>MCP + Allow + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>Hook + MCP + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Hook + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Extension + Instruction</td></tr>
    <tr><td nowrap><b>headroom</b></td><td nowrap>MCP + Allow + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Extension + Instruction</td></tr>
    <tr><td nowrap><b>projectmem</b></td><td nowrap>MCP + Allow + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Instruction</td><td nowrap>MCP + Extension + Instruction</td></tr>
    <tr><td nowrap><b>principles</b></td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td><td nowrap>Instruction</td></tr>
  </tbody>
</table>

### Compression proxy (optional)

headroom's MCP tools compress what an agent hands them. Its proxy compresses
every request an agent sends, which saves more, but it runs as a background
service in front of the API and reroutes agents machine-wide. Tokless asks once
during install and remembers your answer:

```
tokless headroom-proxy on       # install the service and route detected agents
tokless headroom-proxy status   # is it up, and what's routed through it
tokless headroom-proxy off      # remove it; agents go back to direct calls
```

## Usage

```
tokless              Install tools, then pick detected agents (safe to re-run)
tokless update       Update the tokless CLI, then show version diff and upgrade tools
tokless doctor       Show what's wired; warn about broken bits
tokless info         Show how tokless was installed, plus paths and config locations
tokless index        Build per-project indexes (codegraph, projectmem)
tokless disable      Disable one or more agents
tokless uninstall    Remove everything tokless touched, including the CLI itself
tokless self-update  Update the tokless CLI itself
tokless --version    Print tokless version
tokless --help       Show all commands and flags

tokless headroom-proxy on|off|status
                     Compress every request an agent sends
```

Flags:
```
--agents <list>   Subset: claude,opencode,codex,antigravity,copilot,droid,pi
--tools <list>    Subset: rtk,principles,caveman,ponytail,codegraph,context-mode,headroom,projectmem
--headroom-proxy  Turn the compression proxy on without asking
--dry-run         Preview, no writes
--verbose         Every step
--yes             Skip confirmations
```

Restart agents after install so they pick up new config.
