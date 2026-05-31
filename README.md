<div align="center"><h1>tokless: save tokens on AI coding agents, without hurting performance</h1></div>

## Introduction

A unified CLI to install and update every token-saving plugin for your AI coding agents — fast, efficient, and without hurting how the agent performs.

**Supported agents**

- Claude Code
- OpenCode
- Codex

**Supported tools** — each installed from its official source and wired per its own docs:

| Tool | What it does |
| ---- | ------------ |
| [RTK](https://github.com/rtk-ai/rtk) | Shrinks noisy bash/tool output before the model sees it |
| [Caveman](https://github.com/JuliusBrussee/caveman) | Makes the agent answer in terse, token-light prose |
| [CodeGraph](https://github.com/colbymchenry/codegraph) | Lets the agent query a code graph instead of reading whole files |
| [Context-Mode](https://github.com/mksglu/context-mode) | Runs data-heavy work in a sandbox, returns only what matters |

## Install

```bash
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.sh | bash
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.ps1 | iex
```

## Commands

```
tokless              Install + wire everything (default; safe to re-run)
tokless update       Show the version diff and upgrade the four tools
tokless doctor       Show what's wired up; warn about anything broken
tokless uninstall    Remove everything tokless ever touched
tokless self-update  Update the tokless CLI itself
```

Flags:

```
--agents <list>   Limit to a subset: claude,opencode,codex
--dry-run         Show what would change without writing anything
--verbose         Show every step
```

```bash
tokless                              # interactive: pick agents, wire all four tools
tokless --agents opencode --dry-run  # preview, no writes
tokless doctor
```

After running it, restart your agents so they pick up the new config.
