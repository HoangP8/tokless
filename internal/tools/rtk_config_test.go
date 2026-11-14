package tools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

func rtkTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	util.SetHomeOverride(home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Cleanup(func() { util.SetHomeOverride("") })
	return home
}

func writeRtkTestConfig(t *testing.T, content string) string {
	t.Helper()
	path := rtkConfigFile()
	if err := util.WriteFile(path, content); err != nil {
		t.Fatal(err)
	}
	return path
}

func readRtkTestConfig(t *testing.T) string {
	t.Helper()
	raw, ok := util.ReadFileSafe(rtkConfigFile())
	if !ok {
		t.Fatalf("config %s missing", rtkConfigFile())
	}
	return raw
}

func readRtkTestState(t *testing.T) (rtkSuppressState, bool) {
	t.Helper()
	raw, ok := util.ReadFileSafe(rtkSuppressStateFile())
	if !ok {
		return rtkSuppressState{}, false
	}
	var st rtkSuppressState
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		t.Fatalf("state json: %v", err)
	}
	return st, true
}

func pinRtkWired(t *testing.T, wired bool) {
	t.Helper()
	old := rtkHookWired
	rtkHookWired = func() bool { return wired }
	t.Cleanup(func() { rtkHookWired = old })
}

func TestRtkConfigFileLivesUnderUserConfigDir(t *testing.T) {
	home := rtkTestHome(t)
	var base string
	switch runtime.GOOS {
	case "windows":
		base = filepath.Join(home, "AppData", "Roaming")
	case "darwin":
		base = filepath.Join(home, "Library", "Application Support")
	default:
		base = filepath.Join(home, ".config")
	}
	want := filepath.Join(base, "rtk", "config.toml")
	if got := rtkConfigFile(); got != want {
		t.Fatalf("config path = %q want %q", got, want)
	}
	if !strings.HasSuffix(rtkSuppressStateFile(), rtkSuppressStateFN) {
		t.Fatalf("state path = %q", rtkSuppressStateFile())
	}
}

func TestEnsureRtkHookWarningSuppressedCreatesConfig(t *testing.T) {
	rtkTestHome(t)
	changed, err := ensureRtkHookWarningSuppressed()
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got := readRtkTestConfig(t); got != "[hooks]\nsuppress_hook_warning = true\n" {
		t.Fatalf("config = %q", got)
	}
	st, ok := readRtkTestState(t)
	if !ok {
		t.Fatal("state marker missing")
	}
	if !st.FileCreated || !st.TableCreated || st.HadValue {
		t.Fatalf("state = %+v", st)
	}

	changed, err = ensureRtkHookWarningSuppressed()
	if err != nil || changed {
		t.Fatalf("second run changed=%v err=%v", changed, err)
	}
	if got := readRtkTestConfig(t); got != "[hooks]\nsuppress_hook_warning = true\n" {
		t.Fatalf("second run rewrote config: %q", got)
	}
}

func TestEnsureRtkHookWarningSuppressedPreservesUserConfig(t *testing.T) {
	rtkTestHome(t)
	src := "# hand edited\ntelemetry = false\n\n[tracking]\nenabled = true\n\n[hooks]\n# rtk docs\nexclude_commands = [\"curl\"]\n"
	writeRtkTestConfig(t, src)
	if changed, err := ensureRtkHookWarningSuppressed(); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got := readRtkTestConfig(t)
	for _, want := range []string{"# hand edited", "telemetry = false", "[tracking]\nenabled = true", "# rtk docs", "exclude_commands = [\"curl\"]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("lost %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "[hooks]\nsuppress_hook_warning = true\n# rtk docs\nexclude_commands") {
		t.Fatalf("key not inserted in the hooks table:\n%s", got)
	}
	st, _ := readRtkTestState(t)
	if st.FileCreated || st.TableCreated || st.HadValue {
		t.Fatalf("state = %+v", st)
	}
}

func TestEnsureRtkHookWarningSuppressedAdoptsSerializedDefault(t *testing.T) {
	rtkTestHome(t)
	src := "[hooks]\nexclude_commands = []\nsuppress_hook_warning = false\nwrap_fish_scripts = false\n\n[tracking]\nenabled = true\n"
	writeRtkTestConfig(t, src)
	if changed, err := ensureRtkHookWarningSuppressed(); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got := readRtkTestConfig(t); !strings.Contains(got, "suppress_hook_warning = true\n") {
		t.Fatalf("value not adopted:\n%s", got)
	}
	st, ok := readRtkTestState(t)
	if !ok || !st.HadValue || st.Value != "false" || st.TableCreated || st.FileCreated {
		t.Fatalf("state = %+v ok=%v", st, ok)
	}

	pinRtkWired(t, false)
	cleanupRtkHookWarningSuppression()
	got := readRtkTestConfig(t)
	if !strings.Contains(got, "suppress_hook_warning = false\n") {
		t.Fatalf("previous value not restored:\n%s", got)
	}
	if _, ok := readRtkTestState(t); ok {
		t.Fatal("state marker not removed")
	}
}

func TestEnsureRtkHookWarningSuppressedSkipsUnsafeShape(t *testing.T) {
	rtkTestHome(t)
	src := "hooks.exclude_commands = []\n"
	writeRtkTestConfig(t, src)
	changed, err := ensureRtkHookWarningSuppressed()
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got := readRtkTestConfig(t); got != src {
		t.Fatalf("config changed: %q", got)
	}
	if _, ok := readRtkTestState(t); ok {
		t.Fatal("must not claim ownership of an unedited file")
	}
}

func TestEnsureRtkHookWarningSuppressedLeavesExistingTrue(t *testing.T) {
	rtkTestHome(t)
	src := "[hooks]\nsuppress_hook_warning = true\n"
	writeRtkTestConfig(t, src)
	changed, err := ensureRtkHookWarningSuppressed()
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if _, ok := readRtkTestState(t); ok {
		t.Fatal("must not claim a value tokless did not write")
	}
	if got := readRtkTestConfig(t); got != src {
		t.Fatalf("config changed: %q", got)
	}
}

func TestCleanupKeepsFlagWhileRtkStillWired(t *testing.T) {
	rtkTestHome(t)
	if _, err := ensureRtkHookWarningSuppressed(); err != nil {
		t.Fatal(err)
	}
	pinRtkWired(t, true)
	cleanupRtkHookWarningSuppression()
	if _, ok := readRtkTestState(t); !ok {
		t.Fatal("state marker removed while a hook is still wired")
	}
	if got := readRtkTestConfig(t); !strings.Contains(got, "suppress_hook_warning = true") {
		t.Fatalf("flag removed while wired: %q", got)
	}
}

func TestCleanupRemovesFileItCreated(t *testing.T) {
	rtkTestHome(t)
	if _, err := ensureRtkHookWarningSuppressed(); err != nil {
		t.Fatal(err)
	}
	pinRtkWired(t, false)
	cleanupRtkHookWarningSuppression()
	if util.Exists(rtkConfigFile()) {
		raw, _ := util.ReadFileSafe(rtkConfigFile())
		t.Fatalf("created config left behind: %q", raw)
	}
	if util.Exists(rtkSuppressStateFile()) {
		t.Fatal("state marker left behind")
	}
}

func TestCleanupWithoutMarkerLeavesConfig(t *testing.T) {
	rtkTestHome(t)
	src := "[hooks]\nsuppress_hook_warning = true\n"
	writeRtkTestConfig(t, src)
	pinRtkWired(t, false)
	cleanupRtkHookWarningSuppression()
	if got := readRtkTestConfig(t); got != src {
		t.Fatalf("config changed without ownership: %q", got)
	}
}

func TestCleanupRespectsUserChangedValue(t *testing.T) {
	rtkTestHome(t)
	if _, err := ensureRtkHookWarningSuppressed(); err != nil {
		t.Fatal(err)
	}
	writeRtkTestConfig(t, "[hooks]\nsuppress_hook_warning = false\n")
	pinRtkWired(t, false)
	cleanupRtkHookWarningSuppression()
	if got := readRtkTestConfig(t); !strings.Contains(got, "suppress_hook_warning = false") {
		t.Fatalf("user value overwritten: %q", got)
	}
	if _, ok := readRtkTestState(t); ok {
		t.Fatal("state marker kept after respecting user value")
	}
}

func TestWrapRtkHookWarningDryRunWritesNothing(t *testing.T) {
	rtkTestHome(t)
	rtkSuppressHintShown = false
	t.Cleanup(func() { rtkSuppressHintShown = false })

	manifest := fakeRtkManifest()
	wrapRtkHookWarning(manifest)
	ok, err := manifest.WireFor["fake"](core.RunOpts{DryRun: true})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if util.Exists(rtkConfigFile()) || util.Exists(rtkSuppressStateFile()) {
		t.Fatal("dry-run wrote rtk config or marker")
	}
	if !rtkSuppressHintShown {
		t.Fatal("dry-run hint not emitted")
	}
}

func TestWrapRtkHookWarningKeepWiringResultWhenConfigFails(t *testing.T) {
	rtkTestHome(t)
	util.SetWriteFileOverride(func(p, content string) error {
		if p == rtkConfigFile() {
			return errors.New("boom")
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(content), 0o644)
	})
	t.Cleanup(func() { util.SetWriteFileOverride(nil) })

	manifest := fakeRtkManifest()
	wrapRtkHookWarning(manifest)
	ok, err := manifest.WireFor["fake"](core.RunOpts{})
	if !ok || err != nil {
		t.Fatalf("wiring result changed: ok=%v err=%v", ok, err)
	}
	if util.Exists(rtkSuppressStateFile()) {
		t.Fatal("state marker left behind after failed config write")
	}
}

func TestWrapRtkHookWarningLeavesConfigOnFailedWire(t *testing.T) {
	for name, wire := range map[string]core.AgentFn{
		"declined": func(core.RunOpts) (bool, error) { return false, nil },
		"error":    func(core.RunOpts) (bool, error) { return false, errors.New("boom") },
	} {
		t.Run(name, func(t *testing.T) {
			rtkTestHome(t)
			manifest := fakeRtkManifest()
			manifest.WireFor["fake"] = wire
			wrapRtkHookWarning(manifest)
			if ok, err := manifest.WireFor["fake"](core.RunOpts{}); ok {
				t.Fatalf("wiring result changed: ok=%v err=%v", ok, err)
			}
			if util.Exists(rtkConfigFile()) || util.Exists(rtkSuppressStateFile()) {
				t.Fatal("failed wire changed rtk state")
			}
		})
	}
}

// A dangling symlink at the config path must be left alone.
func TestEnsureLeavesDanglingSymlinkAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs a privilege on Windows")
	}
	home := rtkTestHome(t)
	target := filepath.Join(home, "dotfiles", "rtk.toml")
	if err := util.EnsureDir(filepath.Dir(target)); err != nil {
		t.Fatal(err)
	}
	if err := util.EnsureDir(filepath.Dir(rtkConfigFile())); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, rtkConfigFile()); err != nil {
		t.Fatal(err)
	}
	changed, err := ensureRtkHookWarningSuppressed()
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if util.Exists(target) {
		t.Fatal("dangling symlink target created")
	}
	if fi, err := os.Lstat(rtkConfigFile()); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink removed or replaced: %v %v", fi, err)
	}
}

func TestWrapRtkHookWarningUnwireSkipsDryRun(t *testing.T) {
	rtkTestHome(t)
	manifest := fakeRtkManifest()
	wrapRtkHookWarning(manifest)
	if _, err := manifest.WireFor["fake"](core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	pinRtkWired(t, false)
	if _, err := manifest.UnwireFor["fake"](core.RunOpts{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := readRtkTestState(t); !ok {
		t.Fatal("dry-run unwire removed the marker")
	}
}

func TestWrapRtkHookWarningUnwireRestores(t *testing.T) {
	rtkTestHome(t)
	manifest := fakeRtkManifest()
	wrapRtkHookWarning(manifest)
	if _, err := manifest.WireFor["fake"](core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	pinRtkWired(t, false)
	if _, err := manifest.UnwireFor["fake"](core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if util.Exists(rtkConfigFile()) || util.Exists(rtkSuppressStateFile()) {
		t.Fatal("unwire left rtk state behind")
	}
}

func TestWrapRtkHookWarningKeepsFlagUntilLastUnwire(t *testing.T) {
	rtkTestHome(t)
	wired := true
	old := rtkHookWired
	rtkHookWired = func() bool { return wired }
	t.Cleanup(func() { rtkHookWired = old })

	manifest := fakeRtkManifest()
	manifest.WireFor["second"] = manifest.WireFor["fake"]
	manifest.UnwireFor["second"] = manifest.UnwireFor["fake"]
	wrapRtkHookWarning(manifest)

	if _, err := manifest.WireFor["fake"](core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.UnwireFor["fake"](core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := readRtkTestState(t); !ok {
		t.Fatal("flag removed while a second rtk hook remains")
	}
	wired = false
	if _, err := manifest.UnwireFor["second"](core.RunOpts{}); err != nil {
		t.Fatal(err)
	}
	if util.Exists(rtkConfigFile()) || util.Exists(rtkSuppressStateFile()) {
		t.Fatal("last unwire did not restore rtk state")
	}
}

func fakeRtkManifest() *core.ToolManifest {
	return &core.ToolManifest{
		ID:      "rtk-fake",
		WireFor: map[string]core.AgentFn{"fake": func(core.RunOpts) (bool, error) { return true, nil }},
		UnwireFor: map[string]core.AgentFn{"fake": func(core.RunOpts) (bool, error) {
			return true, nil
		}},
	}
}

func TestEnsureAndCleanupPreserveCRLFConfig(t *testing.T) {
	rtkTestHome(t)
	src := "[tracking]\r\nenabled = true\r\n"
	writeRtkTestConfig(t, src)
	if changed, err := ensureRtkHookWarningSuppressed(); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got := readRtkTestConfig(t)
	want := src + "\r\n[hooks]\r\nsuppress_hook_warning = true\r\n"
	if got != want {
		t.Fatalf("after ensure: %q want %q", got, want)
	}
	pinRtkWired(t, false)
	cleanupRtkHookWarningSuppression()
	if got := readRtkTestConfig(t); got != src {
		t.Fatalf("cleanup did not restore the original bytes: %q", got)
	}
}

// The real manifest must be wrapped by package init().
func TestRtkManifestIsWrapped(t *testing.T) {
	rtkTestHome(t)
	rtkSuppressHintShown = false
	t.Cleanup(func() { rtkSuppressHintShown = false })
	wire, ok := rtk.WireFor["zcode"]
	if !ok {
		t.Fatal("zcode wire missing")
	}
	if ok, err := wire(core.RunOpts{DryRun: true}); !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !rtkSuppressHintShown {
		t.Fatal("real manifest is not wrapped: dry-run hint not shown")
	}
	if util.Exists(rtkConfigFile()) {
		t.Fatal("dry-run wrote rtk config")
	}
}

func TestRtkHookWiredFalseInCleanHome(t *testing.T) {
	rtkTestHome(t)
	var wired []string
	for id, verify := range rtk.VerifyFor {
		if verify == nil {
			continue
		}
		if p := verify(); p != nil && *p {
			wired = append(wired, id)
		}
	}
	if len(wired) > 0 {
		t.Fatalf("verifiers report rtk wired in an empty home: %v", wired)
	}
}

func TestCleanupKeepsTableTheUserExtended(t *testing.T) {
	rtkTestHome(t)
	if _, err := ensureRtkHookWarningSuppressed(); err != nil {
		t.Fatal(err)
	}
	if st, _ := readRtkTestState(t); !st.TableCreated {
		t.Fatal("precondition: table should be tokless-created")
	}
	writeRtkTestConfig(t, "[hooks]\nsuppress_hook_warning = true\nexclude_commands = [\"curl\"]\n")
	pinRtkWired(t, false)
	cleanupRtkHookWarningSuppression()
	got := readRtkTestConfig(t)
	if !strings.Contains(got, "exclude_commands = [\"curl\"]") || !strings.Contains(got, "[hooks]") {
		t.Fatalf("user field or table dropped: %q", got)
	}
	if strings.Contains(got, "suppress_hook_warning") {
		t.Fatalf("tokless key not removed: %q", got)
	}
}

func TestCleanupKeepsFileTheUserFilled(t *testing.T) {
	rtkTestHome(t)
	if _, err := ensureRtkHookWarningSuppressed(); err != nil {
		t.Fatal(err)
	}
	if st, _ := readRtkTestState(t); !st.FileCreated {
		t.Fatal("precondition: file should be tokless-created")
	}
	writeRtkTestConfig(t, "[tracking]\nenabled = true\n")
	pinRtkWired(t, false)
	cleanupRtkHookWarningSuppression()
	if got := readRtkTestConfig(t); got != "[tracking]\nenabled = true\n" {
		t.Fatalf("user content lost: %q", got)
	}
}

func TestEnsureRepairsStaleMarkerAfterConfigDeleted(t *testing.T) {
	rtkTestHome(t)
	if _, err := ensureRtkHookWarningSuppressed(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(rtkConfigFile()); err != nil {
		t.Fatal(err)
	}
	changed, err := ensureRtkHookWarningSuppressed()
	if err != nil || !changed {
		t.Fatalf("stale marker blocked re-apply: changed=%v err=%v", changed, err)
	}
	if got := readRtkTestConfig(t); !strings.Contains(got, "suppress_hook_warning = true") {
		t.Fatalf("config not recreated: %q", got)
	}
	if st, ok := readRtkTestState(t); !ok || !st.FileCreated {
		t.Fatalf("marker not refreshed: %+v ok=%v", st, ok)
	}
}

func TestMarkerWithForeignPathIsIgnored(t *testing.T) {
	rtkTestHome(t)
	foreign := filepath.Join(t.TempDir(), "passwd")
	if err := os.WriteFile(foreign, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rtkSuppressState{Path: foreign, HadValue: true, Value: "false"})
	if err != nil {
		t.Fatal(err)
	}
	if err := util.WriteFile(rtkSuppressStateFile(), string(raw)); err != nil {
		t.Fatal(err)
	}
	if changed, err := ensureRtkHookWarningSuppressed(); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got := readRtkTestConfig(t); !strings.Contains(got, "suppress_hook_warning = true") {
		t.Fatalf("config not written: %q", got)
	}
	pinRtkWired(t, false)
	cleanupRtkHookWarningSuppression()
	if b, err := os.ReadFile(foreign); err != nil || string(b) != "x" {
		t.Fatalf("foreign path was touched: %q err=%v", b, err)
	}
}

// A config that exists but cannot be read must never be replaced.
func TestEnsureLeavesUnreadableConfigAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not deny reads on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read mode-000 files")
	}
	rtkTestHome(t)
	src := "[hooks]\nexclude_commands = [\"curl\"]\n"
	path := writeRtkTestConfig(t, src)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	changed, err := ensureRtkHookWarningSuppressed()
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if _, ok := readRtkTestState(t); ok {
		t.Fatal("marker written for a file tokless did not touch")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readRtkTestConfig(t); got != src {
		t.Fatalf("config replaced: %q", got)
	}
}
