package commands

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"

	"github.com/HoangP8/tokless/internal/agents"
	headroompkg "github.com/HoangP8/tokless/internal/headroom"
	"github.com/HoangP8/tokless/internal/util"
)

// ProxyAgentIDs is the full `tokless proxy` agent vocabulary in display order:
func ProxyAgentIDs() []string {
	return []string{
		"claude", "codex", "opencode", "opencode2", "omp", "kilo", "pi", "droid", "antigravity", "grok", "copilot", "cline", "kimi", "zcode", "dsh",
		"cursor",
	}
}

func resolveProxyAgents(opts InitOptions) []string {
	if opts.Agents != nil {
		return opts.Agents
	}
	return ProxyAgentIDs()
}

func completeProxyAgentSelection(ids []string) bool {
	all := ProxyAgentIDs()
	if len(ids) != len(all) {
		return false
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		seen[id] = true
	}
	for _, id := range all {
		if !seen[id] {
			return false
		}
	}
	return true
}

func resolveProxyUpAgents(opts InitOptions) []string {
	ids := resolveProxyAgents(opts)
	var out []string
	for _, id := range ids {
		if id == "copilot" {
			if agents.CopilotCLIProxyApplicable() || agents.CopilotVSCodeProxyApplicable() {
				out = append(out, id)
			}
			continue
		}
		if agents.ProxyAgentApplicable(id) {
			out = append(out, id)
		}
	}
	return out
}

func resolveHeadroomProxyAgents(opts InitOptions) []string {
	var out []string
	for _, id := range resolveProxyAgents(opts) {
		if agents.ProxyAgentUsesHeadroom(id) {
			out = append(out, id)
		}
	}
	return out
}

func validateProxyUpAgents(ids []string) error {
	for _, id := range ids {
		if proxyInstructions(id) != nil {
			continue
		}
		if id == "copilot" {
			if !agents.CopilotCLIProxyApplicable() && !agents.CopilotVSCodeProxyApplicable() {
				continue
			}
		} else if !agents.ProxyAgentApplicable(id) {
			continue
		}
		detection := agents.DetectProxy(id)
		switch detection.State {
		case agents.ProxyStateForeignBYOK, agents.ProxyStateConflict, agents.ProxyStateUnreadable:
			return fmt.Errorf("%s: refusing proxy up with %s state (%s)", id, detection.State, detection.Detail)
		}
	}
	return nil
}

func validateProxyLanePorts() error {
	ports := []struct {
		name string
		port int
	}{
		{"shared Headroom", headroompkg.ProxyPort()},
		{"Grok OAuth", util.GrokOAuthProxyPort()},
		{"Copilot VS Code", agents.CopilotProxyPort()},
		{"Copilot CLI", agents.CopilotCLIProxyPort()},
	}
	seen := make(map[int]string, len(ports))
	for _, lane := range ports {
		if previous, ok := seen[lane.port]; ok {
			return fmt.Errorf("proxy lane ports conflict: %s and %s both use %d", previous, lane.name, lane.port)
		}
		seen[lane.port] = lane.name
	}
	return nil
}

// proxyInstructions returns the manual/env guidance for agents tokless cannot wire.
func proxyInstructions(id string) []string {
	switch id {
	case "cursor":
		return []string{
			"cursor-via-proxy is DEPRECATED — upstream supports manual settings-UI setup only.",
			"Keep cursor on native api2.cursor.sh OAuth.",
		}
	}
	return nil
}

var configureProxyAgent = configureProxyAgentImpl

func configureProxyAgentImpl(id string) bool {
	return agents.ConfigureProxyAgent(id)
}

var removeProxyAgent = removeProxyAgentImpl

var removeProxyAgentChecked = removeProxyAgentCheckedImpl

func removeProxyAgentImpl(id string) bool {
	removed := agents.RemoveProxyAgent(id)
	if removed && proxyAgentWired(id) {
		return false
	}
	return removed
}

func removeProxyAgentCheckedImpl(id string) (bool, error) {
	removed, err := agents.RemoveProxyAgentChecked(id)
	if err == nil && removed && proxyAgentWired(id) {
		return false, nil
	}
	return removed, err
}

var proxyAgentWired = proxyAgentWiredImpl

func proxyAgentWiredImpl(id string) bool {
	return agents.ProxyAgentWired(id)
}

var stopProxy = headroompkg.StopProxy
var startProxy = headroompkg.StartProxy
var enableProxyAutostart = headroompkg.EnableProxyAutostart
var disableProxyAutostart = headroompkg.DisableProxyAutostart
var proxyAutostartEnabled = headroompkg.ProxyAutostartEnabled
var proxyAutostartConfigured = headroompkg.ProxyAutostartConfigured
var proxyRunning = headroompkg.ProxyRunning
var stopCopilotProxy = headroompkg.StopCopilotProxy
var startCopilotProxy = headroompkg.StartCopilotProxy
var copilotProxyRunning = headroompkg.CopilotProxyRunning
var startGrokProxy = headroompkg.StartGrokOAuthProxy
var stopGrokProxy = headroompkg.StopGrokOAuthProxy
var grokProxyOwned = headroompkg.GrokOAuthProxyOwned
var grokProxyRunning = headroompkg.GrokOAuthProxyRunning
var clearStaleGrokProxy = headroompkg.ClearStaleGrokProxyRecord

// RunProxyUp starts the headroom proxy daemon and points agents at it.
func RunProxyUp(opts InitOptions) int {
	cmdHeader("proxy up", "start the headroom HTTP proxy and point agents at it")
	selectedAgents := resolveProxyUpAgents(opts)
	headroomAgents := resolveHeadroomProxyAgents(InitOptions{Agents: selectedAgents})
	grokSelected := hasGrokAgent(selectedAgents) && agents.ProxyAgentApplicable("grok")
	if len(headroomAgents) == 0 && !grokSelected {
		return 0
	}
	if err := validateProxyUpAgents(selectedAgents); err != nil {
		util.L.Err(err.Error())
		return 1
	}
	if err := validateProxyLanePorts(); err != nil {
		util.L.Err(err.Error())
		return 1
	}
	if opts.DryRun {
		if grokSelected {
			util.L.Raw("  " + util.C.Gray("grok: would configure native OAuth and BYOK routing"))
		}
		for _, id := range headroomAgents {
			if proxyInstructions(id) != nil {
				continue
			}
			if id == "copilot" {
				if !agents.CopilotCLIProxyApplicable() && !agents.CopilotVSCodeProxyApplicable() {
					continue
				}
			} else if !agents.ProxyAgentApplicable(id) {
				continue
			}
			if id == "copilot" {
				util.L.Raw("  " + util.C.Gray(id+": would install managed CLI wrapper on port "+strconv.Itoa(agents.CopilotCLIProxyPort())+" and VS Code proxy on port "+strconv.Itoa(agents.CopilotProxyPort())))
			} else {
				util.L.Raw("  " + util.C.Gray(id+": would wire to "+agents.ProxyEndpointFor(id)))
			}
		}
		return 0
	}
	if contains(headroomAgents, "copilot") && (agents.CopilotCLIProxyApplicable() || agents.CopilotVSCodeProxyApplicable()) {
		if err := agents.ValidateCopilotProxyPorts(); err != nil {
			util.L.Err(err.Error())
			return 1
		}
	}
	lifecycleRelease, err := headroompkg.AcquireProxyLifecycleLock()
	if err != nil {
		util.L.Err("proxy lifecycle lock: " + err.Error())
		return 1
	}
	defer lifecycleRelease()
	preferenceSet := util.ProxyRoutingPreferenceSet()
	preferenceEnabled := util.ProxyRoutingEnabled()
	sharedNeeded := len(headroomAgents) > 0 || (grokSelected && agents.GrokProxyUsesHeadroom())
	if sharedNeeded {
		if err := util.SetProxyRoutingEnabled(true); err != nil {
			util.L.Err("proxy preference: " + err.Error())
			return 1
		}
	}
	if sharedNeeded && headroompkg.ResolveHeadroomBin() == "" {
		util.L.Err("headroom binary not found — run `tokless` first to install headroom")
		logProxyRollbackError("proxy preference rollback", restoreProxyPreference(preferenceSet, preferenceEnabled))
		return 1
	}
	wasRunning := proxyRunning()
	sharedStarted := false
	if sharedNeeded {
		if err := startProxy(); err != nil {
			util.L.Err(err.Error())
			logProxyRollbackError("proxy preference rollback", restoreProxyPreference(preferenceSet, preferenceEnabled))
			return 1
		}
		sharedStarted = !wasRunning
	}
	wired, failed := 0, 0
	configured := make([]string, 0)
	agentsBefore := make(map[string]bool)
	grokConfigured := false
	grokStarted := false
	grokRunningBefore := grokProxyRunning()
	if grokSelected {
		wasWired := agents.GrokProxyWired()
		changed, _, configureErr := agents.ConfigureGrokProxyChecked()
		if configureErr != nil {
			util.L.Err("grok: configure failed: " + configureErr.Error())
			if sharedStarted {
				logProxyRollbackError("proxy rollback", stopProxy())
			}
			logProxyRollbackError("proxy preference rollback", restoreProxyPreference(preferenceSet, preferenceEnabled))
			return 1
		}
		if !changed && !wasWired && !agents.GrokProxyWired() {
			if agents.DetectProxy("grok").State != agents.ProxyStateUnconfigured {
				util.L.Err("grok: not wired (differing existing config value, or write failed)")
				if sharedStarted {
					logProxyRollbackError("proxy rollback", stopProxy())
				}
				logProxyRollbackError("proxy preference rollback", restoreProxyPreference(preferenceSet, preferenceEnabled))
				return 1
			}
		}
		grokConfigured = !wasWired && agents.GrokProxyWired()
		if agents.GrokOAuthProxyWired() {
			if err := startGrokProxy(); err != nil {
				util.L.Err("grok OAuth proxy: " + err.Error())
				if !grokRunningBefore && grokProxyOwned() {
					if stopErr := stopGrokProxy(); stopErr != nil {
						util.L.Sub("grok proxy rollback failed: " + stopErr.Error())
					}
				}
				if grokConfigured {
					if !agents.RemoveGrokProxy() {
						util.L.Sub("agent rollback failed: grok")
					}
				}
				if sharedStarted {
					logProxyRollbackError("proxy rollback", stopProxy())
				}
				logProxyRollbackError("proxy preference rollback", restoreProxyPreference(preferenceSet, preferenceEnabled))
				return 1
			}
			grokStarted = !grokRunningBefore
		}
	}
	for _, id := range headroomAgents {
		if proxyInstructions(id) != nil {
			continue
		}
		if id == "copilot" {
			if !agents.CopilotCLIProxyApplicable() && !agents.CopilotVSCodeProxyApplicable() {
				continue
			}
		} else if !agents.ProxyAgentApplicable(id) {
			continue
		}
		if id == "copilot" {
			agentsBefore[id] = agents.CopilotProxyWired()
		} else {
			agentsBefore[id] = proxyAgentWired(id)
		}
		switch {
		case configureProxyAgent(id):
			if id == "copilot" {
				util.L.Ok(id + ": managed Headroom CLI launcher on port " + strconv.Itoa(agents.CopilotCLIProxyPort()))
			} else {
				util.L.Ok(id + ": wired to " + agents.ProxyEndpointFor(id))
			}
			wired++
			if !agentsBefore[id] {
				configured = append(configured, id)
			}
		default:
			if agentsBefore[id] {
				continue
			}
			util.L.Err(id + ": not wired (differing existing config value, or write failed)")
			failed++
		}
	}
	rollback := func() {
		if grokStarted {
			if err := stopGrokProxy(); err != nil {
				util.L.Sub("grok proxy rollback failed: " + err.Error())
			}
		}
		if grokConfigured {
			if !agents.RemoveGrokProxy() {
				util.L.Sub("agent rollback failed: grok")
			}
		}
		for i := len(configured) - 1; i >= 0; i-- {
			id := configured[i]
			if removeProxyAgent(id) {
				continue
			}
			util.L.Sub("agent rollback failed: " + id)
		}
		if sharedStarted {
			if err := stopProxy(); err != nil {
				util.L.Sub("proxy rollback failed: " + err.Error())
			}
		}
	}
	if failed > 0 {
		rollback()
		logProxyRollbackError("proxy preference rollback", restoreProxyPreference(preferenceSet, preferenceEnabled))
		return 1
	}
	if len(headroomAgents) == 0 {
		if grokSelected {
			util.L.Sub("shared Headroom: not needed for Grok OAuth")
		}
	} else if err := enableProxyAutostart(); err != nil {
		if util.ProxyAutostartAllowed() {
			util.L.Sub("autostart: " + err.Error())
		} else {
			util.L.Sub("Starts at login: off (turn on: tokless proxy autostart on)")
		}
		if startErr := startProxy(); startErr != nil {
			util.L.Err(startErr.Error())
			rollback()
			logProxyRollbackError("proxy preference rollback", restoreProxyPreference(preferenceSet, preferenceEnabled))
			return 1
		}
	} else if proxyAutostartEnabled() {
		util.L.Sub(startsAtLoginNotice())
	} else if !proxyRunning() {
		util.L.Err("proxy did not remain running")
		rollback()
		logProxyRollbackError("proxy preference rollback", restoreProxyPreference(preferenceSet, preferenceEnabled))
		return 1
	}
	util.L.Raw("")
	if failed > 0 {
		return 1
	}
	if wired == 0 {
		util.L.Raw("  " + util.C.Gray("No agents wired."))
	}
	if grokSelected {
		util.L.Raw("")
		util.L.Raw("  " + util.C.Dim("Grok CLI OAuth: native and separate from shared Headroom routing."))
	}
	return 0
}

func logProxyRollbackError(action string, err error) {
	if err != nil {
		util.L.Err(action + ": " + err.Error())
	}
}

func restoreProxyPreference(wasSet, wasEnabled bool) error {
	if !wasSet {
		return util.ClearProxyRoutingPreference()
	}
	return util.SetProxyRoutingEnabled(wasEnabled)
}

func hasGrokAgent(ids []string) bool {
	for _, id := range ids {
		if id == "grok" {
			return true
		}
	}
	return false
}

// RunProxyDown unwires agents, then stops the daemon on the complete agent set.
func RunProxyDown(opts InitOptions) int {
	cmdHeader("proxy down", "unwire agents and stop the headroom HTTP proxy")
	if opts.DryRun {
		util.L.Raw("  " + util.C.Gray("dry-run: no changes made"))
		return 0
	}
	lifecycleRelease, err := headroompkg.AcquireProxyLifecycleLock()
	if err != nil {
		util.L.Err("proxy lifecycle lock: " + err.Error())
		return 1
	}
	defer lifecycleRelease()
	failed := 0
	selected := opts.Agents != nil
	proxyAgents := resolveProxyAgents(opts)
	complete := !selected || completeProxyAgentSelection(proxyAgents)
	grokSelected := contains(proxyAgents, "grok")
	preferenceSet := util.ProxyRoutingPreferenceSet()
	preferenceEnabled := util.ProxyRoutingEnabled()
	restorePreference := func() {
		if err := restoreProxyPreference(preferenceSet, preferenceEnabled); err != nil {
			util.L.Sub("proxy preference restore: " + err.Error())
		}
	}
	wiredBefore := map[string]bool{}
	copilotBefore := false
	var copilotSnapshot agents.CopilotProxySnapshot
	if contains(resolveProxyAgents(opts), "copilot") {
		copilotBefore = agents.CopilotProxyWired()
		if copilotBefore {
			copilotSnapshot, err = agents.SnapshotCopilotProxy()
			if err != nil {
				util.L.Err("copilot proxy snapshot: " + err.Error())
				return 1
			}
		}
	}
	sharedRunningBefore := complete && proxyRunning()
	copilotRunningBefore := copilotBefore && copilotProxyRunning()
	grokRunningBefore := grokProxyRunning()
	grokOAuthWiredBefore := agents.GrokOAuthProxyWired()
	grokUsesSharedBefore := agents.GrokProxyUsesHeadroom()
	grokStopped := false
	if grokSelected && grokOAuthWiredBefore && !grokRunningBefore {
		if err := clearStaleGrokProxy(); err != nil {
			util.L.Sub("grok proxy stale record cleanup: " + err.Error())
		}
	}
	autostartBefore := complete && (proxyAutostartEnabled() || proxyAutostartConfigured())
	restoreCopilotWiring := func() {
		if copilotBefore && !agents.CopilotProxyWired() {
			if err := copilotSnapshot.Restore(); err != nil {
				util.L.Sub("agent restore failed: copilot proxy: " + err.Error())
			}
		}
	}
	restoreAll := func() {
		restoreProxyAgents(wiredBefore)
		if grokOAuthWiredBefore && !agents.GrokOAuthProxyWired() {
			if _, _, err := agents.ConfigureGrokProxyChecked(); err != nil {
				util.L.Sub("agent restore failed: grok: " + err.Error())
			}
		}
		restoreCopilotWiring()
		if copilotRunningBefore && !copilotProxyRunning() {
			if err := startCopilotProxy(); err != nil {
				util.L.Sub("Copilot proxy restore: " + err.Error())
			}
		}
		if sharedRunningBefore && !proxyRunning() {
			if err := startProxy(); err != nil {
				util.L.Sub("proxy restore failed: " + err.Error())
			}
		}
		if grokRunningBefore && !grokProxyRunning() {
			if err := startGrokProxy(); err != nil {
				util.L.Sub("grok proxy restore failed: " + err.Error())
			}
		}
		if autostartBefore && !proxyAutostartEnabled() && !proxyAutostartConfigured() {
			if err := enableProxyAutostart(); err != nil {
				util.L.Sub("autostart restore: " + err.Error())
			}
		}
	}
	if (grokSelected || !selected) && grokRunningBefore {
		if err := stopGrokProxy(); err != nil {
			util.L.Err("grok OAuth proxy stop: " + err.Error())
			restoreAll()
			return 1
		}
		grokStopped = true
	}
	for _, id := range proxyAgents {
		if proxyInstructions(id) != nil {
			continue
		}
		if id == "copilot" {
			wiredBefore[id] = copilotBefore
		} else {
			wiredBefore[id] = proxyAgentWired(id)
		}
	}
	for _, id := range proxyAgents {
		if proxyInstructions(id) != nil {
			continue
		}
		if id == "copilot" {
			if !copilotBefore {
				util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" copilot: not wired"))
				continue
			}
			if agents.RemoveCopilotProxy() {
				if err := stopCopilotProxy(); err != nil {
					logProxyRollbackError("copilot proxy rollback", copilotSnapshot.Restore())
					util.L.Err("copilot: private proxy stop failed: " + err.Error())
					failed++
					continue
				}
				util.L.Ok("copilot: proxy wiring removed")
			} else {
				util.L.Err("copilot: unwire failed — removal did not take effect; leaving config untouched")
				failed++
			}
			continue
		}
		if id == "grok" {
			removed, err := removeProxyAgentChecked(id)
			if err != nil {
				util.L.Err("grok: unwire failed: " + err.Error())
				failed++
			} else if removed {
				util.L.Ok("grok: proxy wiring removed")
			} else if wiredBefore[id] {
				util.L.Err("grok: unwire failed — removal did not take effect; leaving config untouched")
				failed++
			} else {
				util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" grok: not wired"))
			}
			continue
		}
		switch {
		case removeProxyAgent(id):
			util.L.Ok(id + ": proxy wiring removed")
		case wiredBefore[id]:
			util.L.Err(id + ": unwire failed — removal did not take effect; leaving config untouched")
			failed++
		default:
			util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" "+id+": not wired"))
		}
	}
	if !complete {
		util.L.Raw("")
		if failed > 0 {
			restoreAll()
			return 1
		}
		if grokSelected && !grokStopped && grokRunningBefore {
			if err := stopGrokProxy(); err != nil {
				util.L.Err("grok OAuth proxy stop: " + err.Error())
				restoreAll()
				return 1
			}
		}
		return 0
	}
	for id, wired := range wiredBefore {
		if !wired {
			continue
		}
		if id == "copilot" {
			if agents.CopilotProxyWired() {
				failed++
			}
			continue
		}
		if proxyAgentWired(id) {
			failed++
		}
	}
	if failed > 0 {
		util.L.Raw("")
		restoreAll()
		return 1
	}
	managedBefore := sharedRunningBefore || grokRunningBefore || copilotRunningBefore ||
		autostartBefore || grokOAuthWiredBefore
	for _, wired := range wiredBefore {
		if wired {
			managedBefore = true
			break
		}
	}
	if !managedBefore {
		util.L.Raw("")
		return 0
	}
	if err := disableProxyAutostart(); err != nil {
		util.L.Sub("autostart: " + err.Error())
		restoreAll()
		util.L.Raw("")
		return 1
	}
	if !grokStopped && grokRunningBefore {
		if err := stopGrokProxy(); err != nil {
			util.L.Err("grok OAuth proxy stop: " + err.Error())
			restoreAll()
			util.L.Raw("")
			return 1
		}
	}
	sharedNeeded := !grokSelected || !grokOAuthWiredBefore || sharedRunningBefore || grokUsesSharedBefore
	for id, wired := range wiredBefore {
		if wired && id != "grok" && id != "copilot" {
			sharedNeeded = true
		}
	}
	if sharedNeeded {
		if err := stopProxy(); err != nil {
			util.L.Err(err.Error())
			restoreAll()
			util.L.Raw("")
			return 1
		}
	}
	if err := util.SetProxyRoutingEnabled(false); err != nil {
		util.L.Err("proxy preference: " + err.Error())
		restoreAll()
		restorePreference()
		util.L.Raw("")
		return 1
	}
	util.L.Raw("")
	return 0
}

func startsAtLoginNotice() string {
	when := "at login"
	if runtime.GOOS == "linux" {
		when = "at login or boot"
	}
	return "Starts " + when + " via " + headroompkg.ProxyAutostartMechanism() + ". Turn off: tokless proxy autostart off"
}

func headroomAgentsWired() bool {
	for _, id := range resolveHeadroomProxyAgents(InitOptions{}) {
		if proxyInstructions(id) != nil {
			continue
		}
		if id == "copilot" {
			if agents.CopilotProxyWired() {
				return true
			}
		} else if proxyAgentWired(id) {
			return true
		}
	}
	return false
}

// setProxyAutostartOff records "do not start at login", removes any existing
// login entry, and restarts a previously running proxy for this session.
func setProxyAutostartOff() (restartErr, err error) {
	release, err := headroompkg.AcquireProxyLifecycleLock()
	if err != nil {
		return nil, fmt.Errorf("proxy lifecycle lock: %w", err)
	}
	defer release()
	wasAllowed := util.ProxyAutostartAllowed()
	wasConfigured := proxyAutostartConfigured()
	if err := util.SetProxyAutostartAllowed(false); err != nil {
		return nil, err
	}
	wasRunning := proxyRunning()
	if err := disableProxyAutostart(); err != nil {
		restoreErr := util.SetProxyAutostartAllowed(wasAllowed)
		if wasAllowed && wasConfigured && !proxyAutostartConfigured() {
			restoreErr = errors.Join(restoreErr, enableProxyAutostart())
		}
		return nil, errors.Join(err, restoreErr)
	}
	if wasRunning && util.ProxyRoutingEnabled() {
		restartErr = startProxy()
	}
	return restartErr, nil
}

// setProxyAutostartOn records "start at login" and registers it when agents
// are already routed through the proxy. applied reports whether it registered.
func setProxyAutostartOn() (applied bool, err error) {
	release, err := headroompkg.AcquireProxyLifecycleLock()
	if err != nil {
		return false, fmt.Errorf("proxy lifecycle lock: %w", err)
	}
	defer release()
	wasAllowed := util.ProxyAutostartAllowed()
	if err := util.SetProxyAutostartAllowed(true); err != nil {
		return false, err
	}
	if !util.ProxyRoutingEnabled() || !headroomAgentsWired() {
		return false, nil
	}
	if err := enableProxyAutostart(); err != nil {
		return false, errors.Join(err, util.SetProxyAutostartAllowed(wasAllowed))
	}
	return true, nil
}

// RunProxyAutostart turns "start the proxy at login" on or off.
func RunProxyAutostart(on bool, opts InitOptions) int {
	cmdHeader("proxy autostart", "start the headroom HTTP proxy at login")
	if opts.DryRun {
		state := "off"
		if on {
			state = "on"
		}
		util.L.Raw("  " + util.C.Gray("dry-run: would turn "+state+" Starts at login. No changes made."))
		return 0
	}
	if on {
		applied, err := setProxyAutostartOn()
		if err != nil {
			util.L.Err("Starts at login: " + err.Error())
			return 1
		}
		if applied {
			util.L.Raw("  " + statusOK("Starts at login: on ("+headroompkg.ProxyAutostartMechanism()+")"))
		} else {
			util.L.Raw("  " + statusOK("Starts at login: on (applies on next `tokless proxy up`)"))
		}
		util.L.Raw("")
		return 0
	}
	restartErr, err := setProxyAutostartOff()
	if err != nil {
		util.L.Err("Starts at login: " + err.Error())
		return 1
	}
	util.L.Raw("  " + statusOK("Starts at login: off"))
	if restartErr != nil {
		util.L.Raw("  " + statusWarn("Proxy stopped. Run: tokless proxy up ("+restartErr.Error()+")"))
	}
	util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" After a reboot the proxy starts when your agent first runs. If requests fail, run: tokless proxy up"))
	util.L.Raw("")
	return 0
}

func restoreProxyAgents(wiredBefore map[string]bool) {
	for id, wired := range wiredBefore {
		if id == "copilot" {
			continue
		}
		if wired && !proxyAgentWired(id) && !configureProxyAgent(id) {
			util.L.Sub("agent restore failed: " + id)
		}
	}
}

func proxyReadinessLabel(r agents.ProxyReadiness) string {
	switch r {
	case agents.ProxyReadyInstallMissing:
		return "headroom not installed"
	case agents.ProxyReadyAgentUnwired:
		return "not wired"
	case agents.ProxyReadyRoutingDisabled:
		return "routing disabled"
	case agents.ProxyReadyManual:
		return "manual"
	}
	return ""
}

// RunProxyStatus prints daemon + per-agent capability and wiring state.
func RunProxyStatus(opts InitOptions) int {
	cmdHeader("proxy status", "headroom HTTP-proxy daemon and agent wiring")
	url := headroompkg.ProxyURL()
	if proxyRunning() {
		util.L.Raw("  " + statusOK("proxy: running on "+url))
	} else {
		util.L.Raw("  " + statusWarn("proxy: not running ("+url+")"))
	}
	switch allowed, configured := util.ProxyAutostartAllowed(), proxyAutostartConfigured() || proxyAutostartEnabled(); {
	case !allowed && configured:
		util.L.Raw("  " + statusWarn("Starts at login: still on ("+headroompkg.ProxyAutostartMechanism()+") although you turned it off. Fix: tokless proxy autostart off"))
	case !allowed:
		util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" Starts at login: off (turn on: tokless proxy autostart on)"))
	case configured:
		util.L.Raw("  " + statusOK("Starts at login: on ("+headroompkg.ProxyAutostartMechanism()+")"))
	default:
		util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" Starts at login: off (run: tokless proxy up)"))
	}
	for _, id := range resolveProxyAgents(opts) {
		detection := agents.DetectProxy(id)
		label := id + ": " + string(detection.Capability.WireKind) + ", " + string(detection.Capability.Protocol) + " — " + string(detection.State)
		if detection.Detail != "" {
			label += " (" + detection.Detail + ")"
		}
		if id != "copilot" && id != "grok" {
			if readiness := agents.ProxyReadinessFor(id); readiness != agents.ProxyReady {
				label += " " + util.C.Dim("["+proxyReadinessLabel(readiness)+"]")
			}
		}
		if detection.State == agents.ProxyStateManaged && detection.Capability.WireKind != agents.ProxyWireManual && id != "copilot" && id != "grok" {
			label += " " + util.C.Dim("→") + " " + agents.ProxyEndpointFor(id)
			util.L.Raw("  " + statusOK(label))
		} else {
			util.L.Raw("  " + util.C.Gray(util.Sym.Bullet+" "+label))
		}
	}
	util.L.Raw("")
	return 0
}
