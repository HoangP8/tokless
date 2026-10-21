package agents

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoangP8/tokless/internal/util"
)

// The OpenCode v2 flow owns its config dir, route namespace and stashes; it never touches v1 state.

// openCodeV2ConfigFiles returns the v2 config files that may hold providers, in load order.
func openCodeV2ConfigFiles() []string {
	paths := util.OpenCodePathsV2()
	return openCodeV2ConfigFilesFor(paths.Dir, paths.Config)
}

// openCodeV2ConfigFilesFor returns the config files of one resolved lane, in load order.
func openCodeV2ConfigFilesFor(dir string, config string) []string {
	paths := []string{
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "opencode.json"),
		filepath.Join(dir, "opencode.jsonc"),
	}
	for _, path := range paths {
		if path == config {
			return paths
		}
	}
	return append(paths, config)
}

// openCodeV2MirrorFile returns the v2 config file, so the mirror never lands in the v1 config.
func openCodeV2MirrorFile() string {
	return util.OpenCodePathsV2().Config
}

// openCodeV2SharesV1Config reports whether v2 and v1 share the config file.
func openCodeV2SharesV1Config() bool {
	return openCodeCleanAbsPath(util.OpenCodePathsV2().Config) == openCodeCleanAbsPath(util.OpenCodePathsResolved().Config)
}

func openCodeCleanAbsPath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return filepath.Clean(abs)
}

// discoverOpenCodeV2LaneBYOK merges the v1-shape and the native v2-shape discoveries of the opencode2 lane.
func discoverOpenCodeV2LaneBYOK() ([]openCodeBYOK, []openCodeSkip) {
	files := openCodeV2ConfigFiles()
	stashPath := byokStashPathFor("opencode2")
	out := discoverOpenCodeBYOKIn(files, "opencode2:", stashPath)
	seen := make(map[string]bool, len(out))
	for _, b := range out {
		seen[b.ID] = true
	}
	native, skipped := discoverOpenCodeV2BYOKIn(files, "opencode2:", stashPath)
	for _, b := range native {
		if seen[b.ID] {
			continue
		}
		seen[b.ID] = true
		out = append(out, b)
	}
	return out, skipped
}

// OpenCodeV2SkippedProviders returns the v2 providers that were not wired, with the reason, so a doctor can surface them.
func OpenCodeV2SkippedProviders() []string {
	_, skipped := discoverOpenCodeV2LaneBYOK()
	return openCodeSkipMessages(skipped)
}

// openCodeV2CommentedConfig returns the comment-bearing v2 config files.
func openCodeV2CommentedConfig() (primary string, secondary []string) {
	primaryPath := util.OpenCodePathsV2().Config
	for _, path := range openCodeV2ConfigFiles() {
		raw, ok := util.ReadFileSafe(path)
		if !ok || !util.HasJSONCComments(raw) {
			continue
		}
		if path == primaryPath {
			primary = path
			continue
		}
		secondary = append(secondary, path)
	}
	return primary, secondary
}

// openCodeV2CommentedConfigError returns the error that stops the v2 wire on a comment-bearing primary config.
func openCodeV2CommentedConfigError() error {
	primary, _ := openCodeV2CommentedConfig()
	if primary == "" {
		return nil
	}
	return fmt.Errorf("unsupported comment-bearing config %s", primary)
}

func openCodeV2TransportTarget(byoks []openCodeBYOK) openCodeTransportTarget {
	return openCodeTransportTarget{
		agentID:         "opencode2",
		file:            util.OpenCodePathsV2().Config,
		prefix:          "opencode2:",
		byoks:           byoks,
		retrieveStash:   openCodeRetrieveStatePathFor("opencode2"),
		transportPlugin: false,
	}
}

// OpenCodeProxyV2Wired reports whether the v2 config mirrors the providers to the proxy and suppresses the retrieve tool.
func OpenCodeProxyV2Wired() bool {
	if openCodeV2SharesV1Config() {
		return OpenCodeProxyWired()
	}
	target := openCodeV2TransportTarget(nil)
	return openCodeV2MirrorWired() && openCodeRetrieveToolDisabledIn(target)
}

// openCodeV2MirrorWired reports whether a mirrored provider points at the proxy.
func openCodeV2MirrorWired() bool {
	providers := openCodeV2ProvidersOf(openCodeV2MirrorFile())
	if providers == nil {
		return false
	}
	endpoint := openCodeProxyEndpointFor("opencode2:")
	for _, id := range providers.Keys() {
		value, _ := providers.Get(id)
		entry, ok := value.(*util.OrderedMap)
		if !ok {
			continue
		}
		if base, _ := openCodeV2ProviderBaseAndKey(entry); base == endpoint {
			return true
		}
	}
	return false
}

// ConfigureOpenCodeProxyV2 wires the OpenCode v2 harness to the headroom proxy.
func ConfigureOpenCodeProxyV2() (changed bool, file string) {
	if openCodeV2SharesV1Config() {
		return ConfigureOpenCodeProxy()
	}
	err := withProxyRouteStashLock(func() error {
		var err error
		changed, file, err = configureOpenCodeProxyV2Locked()
		return err
	})
	if err != nil {
		util.L.Err("opencode2 proxy lock failed: " + err.Error())
	}
	return changed, file
}

func configureOpenCodeProxyV2Locked() (changed bool, file string, resultErr error) {
	file = util.OpenCodePathsV2().Config
	if err := openCodeV2CommentedConfigError(); err != nil {
		return false, file, err
	}
	configRaw, configExists := util.ReadFileSafe(file)
	target := openCodeV2TransportTarget(nil)
	retrieveRaw, retrieveExists := util.ReadFileSafe(target.retrieveStash)
	type routeChange struct {
		id       string
		previous BYOKRoute
	}
	var routeChanges []routeChange
	rollbackRoutes := func() error {
		var errs []error
		for i := len(routeChanges) - 1; i >= 0; i-- {
			route := routeChanges[i]
			if route.previous.ID == "" {
				if err := util.DeleteBYOKRoute(route.id); err != nil {
					errs = append(errs, fmt.Errorf("delete OpenCode v2 route %s: %w", route.id, err))
				}
				continue
			}
			if _, _, err := util.UpsertBYOKRoute(route.id, route.previous.Protocol, route.previous.Upstream); err != nil {
				errs = append(errs, fmt.Errorf("restore OpenCode v2 route %s: %w", route.id, err))
			}
		}
		return errors.Join(errs...)
	}
	byoks, skipped := discoverOpenCodeV2LaneBYOK()
	openCodeV2ReportSkipped(skipped)
	for _, provider := range byoks {
		if provider.Protocol == "" {
			continue
		}
		id := "opencode2:" + provider.ID
		_, previous, err := util.UpsertBYOKRoute(id, provider.Protocol, provider.BaseURL)
		if err != nil {
			return false, file, errors.Join(err, rollbackRoutes())
		}
		routeChanges = append(routeChanges, routeChange{id: id, previous: previous})
	}
	byoks, _ = discoverOpenCodeV2LaneBYOK()
	target = openCodeV2TransportTarget(byoks)
	configured, file := configureOpenCodeTransportPluginIn(target)
	if !configured && !openCodeRetrieveToolDisabledIn(target) {
		return false, file, errors.Join(fmt.Errorf("configure OpenCode v2 retrieve suppression"), rollbackRoutes())
	}
	mirrorFile := openCodeV2MirrorFile()
	stashPath := byokStashPathFor("opencode2")
	stashRaw, stashExists := util.ReadFileSafe(stashPath)
	providers := openCodeV2StashFor(byoks, mirrorFile, "opencode2:")
	if err := saveBYOKStashFileAt(stashPath, byokStashFile{
		Providers: providers,
		Lane:      openCodeV2LaneStashFor(providers, mirrorFile),
	}); err != nil {
		return false, file, errors.Join(fmt.Errorf("record OpenCode v2 BYOK stash"), rollbackRoutes())
	}
	mirrorChanged, ok := syncOpenCodeV2BYOKFor(byoks, mirrorFile, "opencode2:", false)
	if !ok {
		errs := []error{fmt.Errorf("configure OpenCode v2 BYOK providers"), rollbackRoutes()}
		if err := restoreCodexConfig(file, configRaw, configExists); err != nil {
			errs = append(errs, err)
		}
		if err := restoreOpenCodeRetrieveStateAt(target.retrieveStash, retrieveRaw, retrieveExists); err != nil {
			errs = append(errs, err)
		}
		if err := restoreCodexConfig(stashPath, stashRaw, stashExists); err != nil {
			errs = append(errs, err)
		}
		return false, file, errors.Join(errs...)
	}
	return configured || mirrorChanged, file, nil
}

// openCodeV2ReportSkipped warns about the providers that were not wired.
func openCodeV2ReportSkipped(skipped []openCodeSkip) {
	if len(skipped) == 0 {
		return
	}
	util.L.Warn("opencode2 skipped providers: " + strings.Join(openCodeSkipMessages(skipped), ", "))
}

// openCodeV2LaneStashFor records the lane a wire resolved and every file it touched.
func openCodeV2LaneStashFor(providers map[string]byokStashEntry, mirrorFile string) *openCodeLaneStash {
	lane := &openCodeLaneStash{
		Dir:    util.OpenCodePathsV2().Dir,
		Config: mirrorFile,
	}
	previous := loadBYOKStashFileAt(byokStashPathFor("opencode2")).Lane
	if previous != nil {
		lane.Files = append(lane.Files, previous.Files...)
	}
	lane.Files = append(lane.Files, mirrorFile)
	for _, entry := range providers {
		lane.Files = append(lane.Files, entry.File)
	}
	lane.Files = openCodeV2UniquePaths(lane.Files)
	return lane
}

// openCodeV2UniquePaths returns the paths without duplicates, keeping the order.
func openCodeV2UniquePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

// RemoveOpenCodeProxyV2 unwires the OpenCode v2 harness.
func RemoveOpenCodeProxyV2() bool {
	if openCodeV2SharesV1Config() && !openCodeV2LaneRecorded() {
		return RemoveOpenCodeProxy()
	}
	var removed bool
	if err := withProxyRouteStashLock(func() error {
		var err error
		removed, err = removeOpenCodeProxyV2Locked()
		return err
	}); err != nil {
		util.L.Err("opencode2 proxy lock failed: " + err.Error())
		return false
	}
	return removed
}

// openCodeV2LaneRecorded reports whether the v2 stash records a lane of its own.
func openCodeV2LaneRecorded() bool {
	lane := loadBYOKStashFileAt(byokStashPathFor("opencode2")).Lane
	if lane == nil || lane.Config == "" {
		return false
	}
	return openCodeCleanAbsPath(lane.Config) != openCodeCleanAbsPath(util.OpenCodePathsResolved().Config)
}

// openCodeV2UnwireFiles returns the files an unwire walks.
func openCodeV2UnwireFiles(lane *openCodeLaneStash, stashed map[string]byokStashEntry) []string {
	paths := openCodeV2ConfigFiles()
	if lane != nil {
		paths = append(paths, openCodeV2ConfigFilesFor(lane.Dir, lane.Config)...)
		paths = append(paths, lane.Files...)
	}
	for _, entry := range stashed {
		paths = append(paths, entry.File)
	}
	return openCodeV2UniquePaths(paths)
}

func removeOpenCodeProxyV2Locked() (bool, error) {
	stashPath := byokStashPathFor("opencode2")
	if !byokStashValidAt(stashPath) {
		return false, fmt.Errorf("invalid OpenCode v2 BYOK stash")
	}
	stash := loadBYOKStashFileAt(stashPath)
	stashed := stash.Providers
	target := openCodeV2TransportTarget(nil)
	if stash.Lane != nil && stash.Lane.Config != "" {
		target.file = stash.Lane.Config
	}
	if _, exists := util.ReadFileSafe(target.retrieveStash); exists {
		if _, ok := loadOpenCodeRetrieveStateAt(target.retrieveStash); !ok {
			return false, fmt.Errorf("invalid OpenCode v2 retrieve stash")
		}
	}
	routeIDs := map[string]bool{}
	if routes, err := util.ReadBYOKRoutes(); err == nil || os.IsNotExist(err) {
		for _, route := range routes {
			if strings.HasPrefix(route.ID, "opencode2:") {
				routeIDs[route.ID] = true
			}
		}
	} else {
		return false, fmt.Errorf("read BYOK routes")
	}
	byoks, _ := discoverOpenCodeV2LaneBYOK()
	for _, provider := range byoks {
		routeIDs["opencode2:"+provider.ID] = true
	}
	for id := range stashed {
		routeIDs["opencode2:"+id] = true
	}
	configPath := target.file
	configRaw, configExists := util.ReadFileSafe(configPath)
	retrieveRaw, retrieveExists := util.ReadFileSafe(target.retrieveStash)
	byokStashRaw, byokStashExists := util.ReadFileSafe(stashPath)
	providerFiles := map[string]string{}
	for _, route := range stashed {
		if raw, ok := util.ReadFileSafe(route.File); ok {
			providerFiles[route.File] = raw
		}
	}
	mirrorPath := configPath
	mirrorRaw, mirrorExists := util.ReadFileSafe(mirrorPath)
	restoreMirror := func(errs []error) []error {
		if mirrorPath == configPath {
			return errs
		}
		if err := restoreCodexConfig(mirrorPath, mirrorRaw, mirrorExists); err != nil {
			return append(errs, err)
		}
		return errs
	}
	mirrorRemoved := false
	for _, file := range openCodeV2UnwireFiles(stash.Lane, stashed) {
		removed, ok := syncOpenCodeV2BYOKFor(byoks, file, "opencode2:", true)
		if !ok {
			return false, fmt.Errorf("restore OpenCode v2 BYOK providers")
		}
		mirrorRemoved = mirrorRemoved || removed
	}
	pluginRemoved := removeOpenCodeTransportPluginIn(target)
	if !pluginRemoved && openCodeTransportPluginWiredIn(target) {
		var errs []error
		for path, raw := range providerFiles {
			if err := util.WriteFile(path, raw); err != nil {
				errs = append(errs, err)
			}
		}
		if err := restoreCodexConfig(stashPath, byokStashRaw, byokStashExists); err != nil {
			errs = append(errs, err)
		}
		if err := restoreCodexConfig(configPath, configRaw, configExists); err != nil {
			errs = append(errs, err)
		}
		if err := restoreOpenCodeRetrieveStateAt(target.retrieveStash, retrieveRaw, retrieveExists); err != nil {
			errs = append(errs, err)
		}
		return false, errors.Join(append([]error{fmt.Errorf("remove OpenCode v2 transport wiring")}, restoreMirror(errs)...)...)
	}
	for id := range routeIDs {
		if err := util.DeleteBYOKRoute(id); err != nil {
			for path, raw := range providerFiles {
				_ = util.WriteFile(path, raw)
			}
			_ = restoreCodexConfig(stashPath, byokStashRaw, byokStashExists)
			_ = restoreCodexConfig(configPath, configRaw, configExists)
			_ = restoreOpenCodeRetrieveStateAt(target.retrieveStash, retrieveRaw, retrieveExists)
			_ = restoreCodexConfig(mirrorPath, mirrorRaw, mirrorExists)
			return false, fmt.Errorf("delete OpenCode v2 route %s: %w", id, err)
		}
	}
	if err := clearBYOKStashAt(stashPath); err != nil {
		return false, err
	}
	return pluginRemoved || mirrorRemoved, nil
}
