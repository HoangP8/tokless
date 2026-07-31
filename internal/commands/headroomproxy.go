package commands

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/HoangP8/tokless/internal/agents"
	"github.com/HoangP8/tokless/internal/util"
)

// The MCP server compresses payloads an agent chooses to hand it. The proxy
// compresses everything the agent sends, so it saves far more — but it sits in
// front of the API, needs to keep running, and reroutes agents machine-wide.
// So it's opt-in, and headroom's own deploy/install commands own the service:
// they know launchd, systemd and Docker, and they can undo their own work.

const headroomProxyPkg = "headroom-ai[mcp,proxy]"

func proxyDeclinedMarker() string {
	return filepath.Join(util.ToklessDataDir(), "headroom-proxy-declined")
}

func RunHeadroomProxy(mode string) int {
	switch mode {
	case "on":
		return headroomProxyOn()
	case "off":
		return headroomProxyOff()
	case "status", "":
		cmdHeader("headroom-proxy", "compression proxy health")
		return printHeadroomDoctor()
	}
	util.L.Err("Usage: tokless headroom-proxy on|off|status")
	return 2
}

func headroomProxyOn() int {
	cmdHeader("headroom-proxy", "route agent traffic through compression")
	if osEnvTest() {
		_ = os.Remove(proxyDeclinedMarker())
		return 0
	}
	if !util.PyGlobalInstall(headroomProxyPkg, "headroom", true) {
		util.L.Err("Couldn't install the proxy engine.")
		util.L.Sub("Manual: uv tool install --force \"" + headroomProxyPkg + "\"")
		return 1
	}
	if !headroomDeploy(util.ResolvePyBin("headroom")) {
		util.L.Err("headroom deploy failed.")
		util.L.Sub("Manual: headroom deploy --scope user --mode cache --no-docker")
		return 1
	}
	routeAgentsToProxy(true)
	_ = os.Remove(proxyDeclinedMarker())
	util.L.Raw("")
	return printHeadroomDoctor()
}

const headroomProxyURL = "http://127.0.0.1:8787"

// routeAgentsToProxy points agents at the proxy. headroom ships its own
// installer for this, but it rewrites the whole settings file and drops keys
// it doesn't know, so tokless writes the one variable itself.
func routeAgentsToProxy(on bool) {
	url := headroomProxyURL
	if !on {
		url = ""
	}
	if agents.SetClaudeEnv("ANTHROPIC_BASE_URL", url) {
		util.L.Sub("Claude Code routing updated — restart it to pick this up")
	}
}

// headroomDeploy installs the service. headroom prefers a Docker container
// whenever the docker command exists, even when the daemon is down, so tell it
// to use plain Python when Docker can't actually run anything.
func headroomDeploy(hr string) bool {
	// cache mode compresses only what changed between turns, so it doesn't
	// invalidate the prompt cache the agents rely on.
	args := []string{"deploy", "--scope", "user", "--mode", "cache", "--providers", "auto"}
	if dockerUsable() {
		if util.Run(hr, args, util.RunOptions{}).Code == 0 {
			return true
		}
		util.L.Sub("retrying without Docker")
	}
	return util.Run(hr, append(args, "--no-docker"), util.RunOptions{}).Code == 0
}

func dockerUsable() bool {
	d := util.Which("docker")
	if d == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return util.Run(d, []string{"info"}, util.RunOptions{Capture: true, Quiet: true, Ctx: ctx}).Code == 0
}

func headroomProxyOff() int {
	cmdHeader("headroom-proxy", "stop routing agent traffic")
	// Unroute before removing the proxy, or agents point at a dead port.
	routeAgentsToProxy(false)
	if !osEnvTest() {
		util.HeadroomProxyRemove()
	}
	_ = util.EnsureDir(util.ToklessDataDir())
	_ = util.WriteFile(proxyDeclinedMarker(), "off\n")
	util.L.Raw("  " + util.C.Green(util.Sym.Check) + " " + util.C.Gray("Proxy removed. Agents talk to the API directly again."))
	util.L.Raw("")
	return 0
}

// headroomProxyTeardown removes the service before the binary that manages it
// goes away, or the agents stay pointed at a proxy nothing can restart.
func headroomProxyTeardown() {
	if osEnvTest() {
		return
	}
	routeAgentsToProxy(false)
	if util.HeadroomProxyDeployed() {
		util.HeadroomProxyRemove()
	}
	_ = os.Remove(proxyDeclinedMarker())
}

type headroomCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Hint    string `json:"hint"`
}

type headroomHealth struct {
	Port     int             `json:"port"`
	Version  string          `json:"installed_version"`
	ExitCode int             `json:"exit_code"`
	Checks   []headroomCheck `json:"checks"`
}

// headroomDoctor: exit_code is 0 healthy, 1 warnings, 2 proxy down.
func headroomDoctor() (headroomHealth, bool) {
	hr := util.ResolvePyBin("headroom")
	if hr == "" {
		return headroomHealth{}, false
	}
	r := util.Run(hr, []string{"doctor", "--json"}, util.RunOptions{Capture: true, Quiet: true})
	var h headroomHealth
	if json.Unmarshal([]byte(r.Stdout), &h) != nil {
		return headroomHealth{}, false
	}
	return h, true
}

func printHeadroomDoctor() int {
	if osEnvTest() {
		return 0
	}
	h, ok := headroomDoctor()
	if !ok {
		util.L.Err("headroom isn't installed. Run: tokless")
		util.L.Raw("")
		return 1
	}
	util.TreeTop("Proxy")
	for _, c := range h.Checks {
		util.TreeLeaf(headroomCheckLine(c))
	}
	util.TreeClose()
	switch {
	case h.ExitCode == 0:
		treeStatus(statusOK("Compression proxy is live."))
	case h.ExitCode == 1:
		// Running, but something isn't routed through it. Agents still work.
		treeStatus(statusOK("Compression proxy is live."),
			statusInfo(util.C.Gray("Some agents aren't routed — the warnings above say which.")))
	default:
		treeStatus(statusWarn("Proxy is down — run ") + paintCmd("tokless headroom-proxy on"))
	}
	util.L.Raw("")
	if h.ExitCode >= 2 {
		return 1
	}
	return 0
}

// checkColumn keeps the summary in its own column. headroom has check names
// longer than the column, which would otherwise run into the text.
func checkColumn(name string) string {
	if len(name) >= 14 {
		return name + "  "
	}
	return padEnd(name, 14)
}

func headroomCheckLine(c headroomCheck) string {
	name := paintName(checkColumn(c.Name))
	switch c.Status {
	case "pass":
		return util.C.Green(util.Sym.Check) + " " + name + util.C.Green(c.Summary)
	case "warn":
		return util.C.Yellow(util.Sym.Warn) + " " + name + util.C.Yellow(c.Summary)
	case "fail":
		line := util.C.Red(util.Sym.Cross) + " " + name + util.C.Red(c.Summary)
		if c.Hint != "" {
			line += util.C.Gray(" — " + c.Hint)
		}
		return line
	}
	return util.C.Gray(util.Sym.Bullet+" ") + util.C.Dim(checkColumn(c.Name)) + util.C.Gray(c.Summary)
}

// headroomProxyStatusLine is doctor's one row for the proxy. Empty when it was
// never turned on — nobody needs nagging about a feature they declined.
func headroomProxyStatusLine() string {
	if osEnvTest() || !util.HeadroomProxyDeployed() {
		return ""
	}
	name := paintName(padEnd("proxy", 14))
	h, ok := headroomDoctor()
	if !ok || h.ExitCode >= 2 {
		return util.C.Yellow(util.Sym.Warn) + " " + name +
			util.C.Yellow("down") + util.C.Gray(" — run ") + paintCmd("tokless headroom-proxy on")
	}
	return util.C.Green(util.Sym.Check) + " " + name + util.C.Green("compressing agent traffic")
}

// maybeEnableHeadroomProxy asks once, on the first install that could use it.
func maybeEnableHeadroomProxy(opts InitOptions) {
	if osEnvTest() || opts.DryRun || util.ResolvePyBin("headroom") == "" {
		return
	}
	if util.Exists(proxyDeclinedMarker()) || util.HeadroomProxyDeployed() {
		return
	}
	if opts.HeadroomProxy {
		_ = RunHeadroomProxy("on")
		return
	}
	// Rerouting every agent's API traffic isn't something to do to someone who
	// isn't watching.
	if !util.IsInteractive() || opts.Yes {
		util.L.Raw("  " + util.C.Gray("Bigger savings available: ") + paintCmd("tokless headroom-proxy on"))
		return
	}
	util.L.Raw("")
	if util.Confirm("Route agent traffic through headroom's proxy? Compresses every request, not just the ones an agent hands over.", true) {
		_ = RunHeadroomProxy("on")
		return
	}
	_ = util.EnsureDir(util.ToklessDataDir())
	_ = util.WriteFile(proxyDeclinedMarker(), "off\n")
	util.L.Raw("  " + util.C.Gray("Skipped. Turn it on later with ") + paintCmd("tokless headroom-proxy on"))
}
