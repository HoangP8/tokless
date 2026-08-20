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

func headroomInstallArgs(upgrade bool) []string {
	args := []string{"tool", "install"}
	if upgrade {
		args = append(args, "--upgrade")
	}
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
		hint = " Rust/native toolchain may be required on this platform. Manual: " + strings.Join(append([]string{util.HeadroomPathsResolved().UV}, headroomInstallArgs(false)...), " ")
	}
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail != "" {
		detail = ": " + strings.Split(detail, "\n")[0]
	}
	return fmt.Errorf("headroom %s failed%s%s", stage, detail, hint)
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

func headroomVersionProbe() error {
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
		return false, headroomFailure("package install", result)
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
