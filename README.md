<div align="center">

<h1>tokless</h1>

**Save tokens on AI coding agents — no performance loss.**

One tool, no config — works the moment it lands.

![version](https://img.shields.io/github/v/release/HoangP8/tokless?label=version)
![platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-blue)
![license](https://img.shields.io/badge/license-MIT-green)

</div>

## Introduction

A unified CLI to install and update every token-saving plugin — RTK, Caveman, CodeGraph, and Context-Mode — for your AI coding agents, fast, efficient, and without hurting how the agent performs.

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

Each tool targets a different source of token waste, so they complement each other with no overlap or conflict.

## Install

<div align="center">

<img src="assets/install.svg" alt="tokless install: run one curl command, then pick which agents to wire" width="100%" />

</div>

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.sh | bash
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/HoangP8/tokless/main/scripts/install.ps1 | iex
```

## Commands

```
tokless              Install + wire everything (default; safe to re-run)
tokless update       Show the version diff and upgrade the four tools
tokless doctor       Show what's wired up; warn about anything broken
tokless index        Build per-project indexes (codegraph) in the current dir
tokless uninstall    Remove everything tokless ever touched
tokless self-update  Update the tokless CLI itself
```

Flags:

```
--agents <list>   Limit to a subset: claude,opencode,codex
--tools <list>    Limit to a subset: rtk,caveman,codegraph,context-mode
--dry-run         Show what would change without writing anything
--verbose         Show every step
```

```bash
tokless                              # interactive: pick agents, wire all four tools
tokless --agents opencode --dry-run  # preview, no writes
tokless doctor
```

After running it, restart your agents so they pick up the new config.
