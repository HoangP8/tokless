package tools

import (
	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

// headroom is the last step of the chain: codegraph avoids reading source,
// context-mode keeps big files out, rtk trims shell output, and headroom
// squeezes whatever still has to go in.
//
// MCP server only. Upstream's hooks just warm up a daemon used by its proxy
// mode, which we don't wire, so they'd only slow down every Bash call. We also
// skip `headroom mcp install` — it writes the same configs tokless owns.
//
// ponytail: proxy mode saves the most but is global, hard to undo, and can't
// be checked per agent. Left out until there's a flag for it.

// Only the MCP server extra. "all" would also pull image, voice and model
// training packages — hundreds of megabytes we never run.
const headroomPkg = "headroom-ai[mcp]"

// With the proxy running, keep its engine installed and restart the service
// after an upgrade — otherwise it keeps serving the old version.
const headroomProxyPkg = "headroom-ai[mcp,proxy]"

func headroomEnsureInstalled(opts core.RunOpts) (bool, error) {
	if isTest() {
		return true, nil
	}
	if opts.DryRun {
		util.L.Sub("[dry-run] would install " + headroomPkg + " (uv/pipx/pip)")
		return true, nil
	}
	opts.Reportf("checking", 0.1)
	if util.ResolvePyBin("headroom") != "" && !opts.Upgrade {
		opts.Reportf("already installed", 1)
		return true, nil
	}
	pkg := headroomPkg
	proxied := util.HeadroomProxyDeployed()
	if proxied {
		pkg = headroomProxyPkg
	}
	opts.Reportf("installing "+pkg, 0.4)
	if !util.PyGlobalInstall(pkg, "headroom", opts.Upgrade) {
		util.L.Err("headroom install failed across uv, pipx and pip.")
		util.L.Sub("Manual: uv tool install \"" + pkg + "\" — https://docs.headroomlabs.ai/docs")
		return false, nil
	}
	if proxied {
		opts.Reportf("restarting proxy", 0.9)
		util.HeadroomProxyRestart()
	}
	opts.Reportf("ready", 1)
	return true, nil
}

func headroomReady() bool { return isTest() || util.ResolvePyBin("headroom") != "" }

var headroomWireFor, headroomUnwireFor, headroomVerifyFor = mcpAgentMaps("headroom", headroomReady)

var headroom = &core.ToolManifest{
	ID:             "headroom",
	Label:          "Headroom",
	Description:    "Compresses tool outputs and payloads before they reach the model.",
	Homepage:       "https://github.com/headroomlabs-ai/headroom",
	InstallHint:    "uv tool install \"headroom-ai[mcp]\"",
	Channel:        core.ChannelPyPI,
	Pkg:            headroomPkg,
	Bin:            "headroom",
	NeedsPython:    true,
	MinPythonMinor: 10,
	Install:        headroomEnsureInstalled,
	WireFor:        headroomWireFor,
	UnwireFor:      headroomUnwireFor,
	VerifyFor:      headroomVerifyFor,
}
