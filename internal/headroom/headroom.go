package headroom

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/HoangP8/tokless/internal/core"
	"github.com/HoangP8/tokless/internal/util"
)

const headroomPackage = "headroom-ai[proxy]"
const headroomVersionTimeout = 30 * time.Second

var runHeadroom = func(command string, args, env []string, ctx context.Context) util.ExecResult {
	return util.Run(command, args, util.RunOptions{Capture: true, Env: env, Ctx: ctx})
}

func headroomInstallArgs(upgrade bool, extra ...string) []string {
	args := []string{"tool", "install"}
	if upgrade {
		args = append(args, "--upgrade")
	}
	args = append(args, extra...)
	return append(args, "--python", "3.13", headroomPackage)
}
func headroomPythonInstallArgs() []string { return []string{"python", "install", "3.13"} }
func headroomNativeBuildRisk() bool       { return headroomNativeBuildRiskFor(runtime.GOOS, runtime.GOARCH) }
func headroomNativeBuildRiskFor(goos, goarch string) bool {
	return goos == "windows" || goos == "darwin" && goarch == "amd64"
}

func headroomFailure(stage string, result util.ExecResult) error {
	return headroomFailureFor(stage, result, headroomNativeBuildRisk())
}

func headroomFailureFor(stage string, result util.ExecResult, nativeRisk bool) error {
	hint := ""
	if nativeRisk {
		hint = " Rust/native toolchain may be required on this platform. Install it, then re-run: tokless update --tools headroom"
	}
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail != "" {
		detail = ": " + headroomDetailLine(detail)
	}
	return fmt.Errorf("headroom %s failed%s%s", stage, detail, hint)
}

// headroomDetailLine picks the most informative line of uv output. uv prints
// "Resolved N packages" first and the real failure ("× Failed to build ...",
// "error: ...") later, so prefer those over line 1.
func headroomDetailLine(detail string) string {
	lines := strings.Split(detail, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "×") || strings.HasPrefix(strings.ToLower(l), "error") {
			return l
		}
	}
	return strings.TrimSpace(lines[0])
}

// headroomUVBootstrapCmd picks the official Astral uv installer for the OS.
// Unix: curl preferred, wget fallback. Windows: powershell irm|iex.
// UV_INSTALL_DIR / UV_NO_MODIFY_PATH must already be in the process env (see HeadroomUVBootstrapEnv).
func headroomUVBootstrapCmd(windows, haveCurl, haveWget bool) (string, []string, error) {
	if windows {
		return "powershell", []string{
			"-NoProfile",
			"-ExecutionPolicy", "Bypass",
			"-Command",
			"irm https://astral.sh/uv/install.ps1 | iex",
		}, nil
	}
	if haveCurl {
		return "sh", []string{"-c", "curl -LsSf https://astral.sh/uv/install.sh | sh"}, nil
	}
	if haveWget {
		return "sh", []string{"-c", "wget -qO- https://astral.sh/uv/install.sh | sh"}, nil
	}
	return "", nil, fmt.Errorf("need curl or wget to bootstrap uv")
}

func headroomUV() (string, error) {
	p := util.HeadroomPathsResolved()
	if util.Exists(p.UV) && headroomUVWorks(p.UV) {
		return p.UV, nil
	}
	if system := util.Which("uv"); system != "" && headroomUVWorks(system) {
		return system, nil
	}
	if err := util.EnsureDir(filepath.Dir(p.UV)); err != nil {
		return "", err
	}
	command, args, err := headroomUVBootstrapCmd(util.IsWin, util.Which("curl") != "", util.Which("wget") != "")
	if err != nil {
		return "", err
	}
	env := util.HeadroomUVBootstrapEnv()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result := runHeadroom(command, args, env, ctx)
	if result.Code != 0 || !util.Exists(p.UV) || !headroomUVWorks(p.UV) {
		return "", headroomFailure("uv bootstrap", result)
	}
	return p.UV, nil
}

func headroomUVWorks(uv string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return runHeadroom(uv, []string{"--version"}, util.HeadroomEnv(), ctx).Code == 0
}

// headroomVersionProbe verifies the installed headroom executable.
func headroomVersionProbe() error {
	if util.HeadroomMetadataVersion() != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), headroomVersionTimeout)
	defer cancel()
	result := runHeadroom(util.HeadroomBin(), []string{"--version"}, nil, ctx)
	if result.Code != 0 {
		return headroomFailure("executable verification", result)
	}
	return nil
}

func EnsureInstalled(opts core.RunOpts) (bool, error) {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return true, nil
	}
	if opts.DryRun {
		util.L.Sub("[dry-run] would install headroom-ai[proxy] with managed Python 3.13")
		return true, nil
	}
	opts.Reportf("uv bootstrap", 0.1)
	uv, err := headroomUV()
	if err != nil {
		return false, err
	}
	if util.HeadroomInstalled() && !opts.Upgrade {
		if err := headroomVersionProbe(); err != nil {
			return false, err
		}
		opts.Reportf("already installed", 1)
		return true, nil
	}
	opts.Reportf("managed Python", 0.3)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if result := runHeadroom(uv, headroomPythonInstallArgs(), util.HeadroomEnv(), ctx); result.Code != 0 {
		return false, headroomFailure("managed Python", result)
	}
	opts.Reportf("package install", 0.5)
	result := runHeadroom(uv, headroomInstallArgs(opts.Upgrade), util.HeadroomEnv(), ctx)
	if result.Code != 0 {
		// A dependency whose newest release has no wheel for this platform needs
		// a native (Rust) build. Retry on prebuilt wheels only: uv then resolves
		// to versions that ship one. The first failure stays the reported error.
		// The retry gets its own deadline: a failed source build can eat the
		// shared one.
		opts.Reportf("package install (prebuilt wheels)", 0.6)
		retryCtx, retryCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer retryCancel()
		if retry := runHeadroom(uv, headroomInstallArgs(opts.Upgrade, "--no-build"), util.HeadroomEnv(), retryCtx); retry.Code != 0 {
			return false, headroomFailure("package install", result)
		}
		util.L.Sub("headroom installed from prebuilt wheels only; some dependencies may be older than the latest releases")
	}
	if !util.HeadroomInstalled() {
		return false, fmt.Errorf("headroom executable verification failed: %s", util.HeadroomBin())
	}
	if err := headroomVersionProbe(); err != nil {
		return false, err
	}
	opts.Reportf("ready", 1)
	return true, nil
}
