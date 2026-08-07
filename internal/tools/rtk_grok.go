package tools

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

const grokRtkMarker = "tokless-grok-rtk-v2"

// grokRtkCmds are shell functions defined by the helper so that commands run
// inside grok's persistent shell get rtk output compaction.
var grokRtkCmds = []string{
	"git", "gh", "glab", "cargo", "docker", "find", "jest", "kubectl",
	"lint", "ls", "next", "npm", "npx", "oc", "playwright", "pnpm",
	"prettier", "prisma", "tsc", "vitest",
}

func grokRtkConfig() string {
	dir := os.Getenv("GROK_HOME")
	if dir == "" {
		dir = filepath.Join(util.Home(), ".grok")
	}
	return filepath.Join(dir, "config.toml")
}

func grokRtkMarkerStart() string { return "# >>> " + grokRtkMarker + " >>>" }
func grokRtkMarkerEnd() string   { return "# <<< " + grokRtkMarker + " <<<" }

// grokRtkHelperPath is the single tokless-owned script sourced by grok's
// cmd_prefix.
func grokRtkHelperPath() string {
	return filepath.Join(util.ToklessDataDir(), "grok-rtk.sh")
}

// grokRtkHelper renders the shared helper: one bash-3.2 + zsh compatible
// implementation, sourced via grok's cmd_prefix into its persistent shell.
func grokRtkHelper(rtkPath string) string {
	var b strings.Builder
	b.WriteString("# " + grokRtkMarker + " (sourced via grok's [toolset.bash] cmd_prefix)\n")
	b.WriteString("_tokless_ea=\"\"\n")
	b.WriteString("if [ -n \"${BASH_VERSION:-}\" ]; then\n")
	b.WriteString("  shopt -q expand_aliases 2>/dev/null && _tokless_ea=1\n")
	b.WriteString("  shopt -u expand_aliases\n")
	b.WriteString("elif [ -n \"${ZSH_VERSION:-}\" ]; then\n")
	b.WriteString("  [[ -o aliases ]] && _tokless_ea=1\n")
	b.WriteString("  unsetopt aliases\n")
	b.WriteString("fi\n")
	b.WriteString("_tokless_rtk() {\n")
	b.WriteString("  local _cmd=\"$1\"; shift\n")
	b.WriteString("  local _rtk _out _qs _a _rc\n")
	b.WriteString("  _rtk=\"$(command -v rtk 2>/dev/null || printf '%s\\n' " + shQuote(rtkPath) + ")\"\n")
	b.WriteString("  if [ -n \"$_rtk\" ]; then\n")
	b.WriteString("    _qs=\"$_cmd\"\n")
	b.WriteString("    for _a in \"$@\"; do _qs=\"$_qs $(printf '%q' \"$_a\")\"; done\n")
	b.WriteString("    _rc=0\n")
	b.WriteString("    _out=\"\"\n")
	b.WriteString("    _out=\"$($_rtk rewrite \"$_qs\" 2>/dev/null)\" || _rc=$?\n")
	b.WriteString("    if [ \"$_rc\" = 3 ] && [ -n \"$_out\" ]; then\n")
	b.WriteString("      eval \"$_rtk ${_out#rtk }\"\n")
	b.WriteString("      return\n")
	b.WriteString("    fi\n")
	b.WriteString("  fi\n")
	b.WriteString("  command \"$_cmd\" \"$@\"\n")
	b.WriteString("}\n")
	for _, c := range grokRtkCmds {
		b.WriteString("unalias " + c + " 2>/dev/null || true\n")
		b.WriteString(c + "() { _tokless_rtk " + c + " \"$@\"; }\n")
	}
	b.WriteString("if [ -n \"${BASH_VERSION:-}\" ]; then\n")
	b.WriteString("  [ \"$_tokless_ea\" = 1 ] && shopt -s expand_aliases\n")
	b.WriteString("elif [ -n \"${ZSH_VERSION:-}\" ]; then\n")
	b.WriteString("  [ \"$_tokless_ea\" = 1 ] && setopt aliases\n")
	b.WriteString("fi\n")
	b.WriteString("unset _tokless_ea\n")
	return b.String()
}

// grokRtkCmdPrefix renders the [toolset.bash] cmd_prefix that sources the
// helper into grok's persistent shell before every command.
func grokRtkCmdPrefix(helper string) string {
	return "source " + shQuote(helper)
}

func grokRtkConfigBlock(helper string) string {
	return grokRtkMarkerStart() + "\n" +
		"[toolset.bash]\n" +
		"cmd_prefix = \"" + grokTomlBasicQuote(grokRtkCmdPrefix(helper)) + "\"\n" +
		grokRtkMarkerEnd() + "\n"
}

// grokTomlBasicQuote escapes a string for a TOML basic (double-quoted) string.
func grokTomlBasicQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func removeGrokRtkBlock(raw string) string {
	si, ei := strings.Index(raw, grokRtkMarkerStart()), strings.Index(raw, grokRtkMarkerEnd())
	if si < 0 || ei < si {
		return raw
	}
	ei += len(grokRtkMarkerEnd())
	return strings.TrimRight(raw[:si], "\n") + "\n" + strings.TrimLeft(raw[ei:], "\n")
}

// grokRtkWriteHelper writes the helper atomically under tokless's data dir.
func grokRtkWriteHelper(rtkPath string) error {
	dir := filepath.Dir(grokRtkHelperPath())
	if err := util.EnsureDir(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tokless-grok-rtk-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(grokRtkHelper(rtkPath)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil || tmp.Close() != nil {
		return err
	}
	return os.Rename(tmpPath, grokRtkHelperPath())
}

func grokRtkVerify() bool {
	if _, ok := util.ReadFileSafe(grokRtkHelperPath()); !ok {
		return false
	}
	raw, ok := util.ReadFileSafe(grokRtkConfig())
	return ok && strings.Contains(raw, grokRtkMarkerStart()) &&
		strings.Contains(raw, grokRtkMarkerEnd()) &&
		strings.Contains(raw, "cmd_prefix")
}

func grokRtkWire(opts core.RunOpts) (bool, error) {
	if opts.DryRun {
		util.L.Sub("[dry-run] would add [toolset.bash] cmd_prefix sourcing " + grokRtkHelperPath() + " to ~/.grok/config.toml (no rc edits)")
		return true, nil
	}
	if util.IsWin {
		util.L.Err("grok rtk cmd_prefix requires a bash/zsh login shell (unsupported on Windows)")
		return false, nil
	}
	rtkPath := ""
	if os.Getenv("TOKLESS_TEST") != "1" {
		rtkPath = util.ResolveRtkBin()
		if rtkPath == "" {
			util.L.Err("rtk binary not found on PATH or known install dirs")
			return false, nil
		}
	}
	if err := grokRtkWriteHelper(rtkPath); err != nil {
		util.L.Err("failed to write " + grokRtkHelperPath() + ": " + err.Error())
		return false, nil
	}
	raw, _ := util.ReadFileSafe(grokRtkConfig())
	raw = removeGrokRtkBlock(raw)
	if raw != "" && !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	if err := util.WriteFile(grokRtkConfig(), raw+grokRtkConfigBlock(grokRtkHelperPath())); err != nil {
		util.L.Err("failed to update " + grokRtkConfig() + ": " + err.Error())
		return false, nil
	}
	return grokRtkVerify(), nil
}

func grokRtkUnwire(core.RunOpts) (bool, error) {
	raw, ok := util.ReadFileSafe(grokRtkConfig())
	if ok {
		_ = util.WriteFile(grokRtkConfig(), removeGrokRtkBlock(raw))
	}
	_ = os.Remove(grokRtkHelperPath())
	return !grokRtkVerify(), nil
}
