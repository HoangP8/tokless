package util

import (
	"os"
	"path/filepath"
	"strings"
)

// EnsureNodeForTools makes sure usable npm+npx exist, offering to install Node LTS.
func EnsureNodeForTools() bool {
	if nodeToolsReady() {
		return true
	}
	L.Warn("CodeGraph and Context-Mode need Node.js (npm/npx), which isn't installed.")
	if !Confirm("Install the latest Node.js LTS now?", true) {
		L.Info("Skipping Node install. Install it later: https://nodejs.org/en/download")
		L.Info("Then re-run: tokless")
		return false
	}
	if installNode() && nodeToolsReady() {
		L.Ok("Node.js installed.")
		return true
	}
	L.Err("Node install didn't complete. Install manually: https://nodejs.org/en/download")
	return false
}

func EnsureGitForTools() bool {
	if Which("git") != "" {
		return true
	}
	L.Warn("Caveman needs git (npm github: installs and skills clones), which isn't installed.")
	if !IsWin {
		L.Info("Install git with your package manager (apt/dnf/brew), then re-run: tokless")
		return false
	}
	if !Confirm("Install MinGit (portable, ~30 MB) now?", true) {
		L.Info("Skipping git install. Get it later: https://git-scm.com/downloads")
		return false
	}
	if installGitWindowsZip() && Which("git") != "" {
		L.Ok("MinGit installed.")
		return true
	}
	L.Err("Git install didn't complete. Install manually: https://git-scm.com/downloads")
	return false
}

// nodeToolsReady reports usable npm+npx.
func nodeToolsReady() bool {
	npm, npx := Which("npm"), Which("npx")
	if npm == "" || npx == "" {
		return false
	}
	if isWSL() && (isWindowsMount(npm) || isWindowsMount(npx)) {
		L.Warn("Found Windows Node (" + npm + ") in WSL — a Linux Node install is needed here.")
		return false
	}
	return true
}

func isWSL() bool {
	return !IsWin && (os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "")
}

// isWindowsMount matches WSL drvfs paths (/mnt/c/...), not arbitrary /mnt dirs.
func isWindowsMount(p string) bool {
	return len(p) > 6 && strings.HasPrefix(p, "/mnt/") && p[6] == '/' &&
		p[5] >= 'a' && p[5] <= 'z'
}

func installNode() bool {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return true
	}
	if IsWin {
		return installNodeWindows()
	}
	return installNodeUnix()
}

func installNodeUnix() bool {
	if Which("curl") == "" {
		L.Err("Need curl to install Node.")
		return false
	}
	fnmHome := os.Getenv("HOME") + "/.local/share/fnm"
	if Run("sh", []string{"-c", "curl -fsSL https://fnm.vercel.app/install | bash -s -- --skip-shell"}, RunOptions{}).Code != 0 {
		return false
	}
	r := Run("sh", []string{"-c",
		`eval "$(` + fnmHome + `/fnm env --shell bash)" && ` + fnmHome + `/fnm install --lts && ` + fnmHome + `/fnm use lts-latest && echo "$PATH"`,
	}, RunOptions{Capture: true})
	if r.Code != 0 {
		return false
	}
	if p := strings.TrimSpace(r.Stdout); p != "" {
		os.Setenv("PATH", p)
	}
	return Which("node") != "" && Which("npm") != ""
}

func installNodeWindows() bool {
	if Which("winget") != "" {
		r := Run("winget", []string{"install", "-e", "--id", "OpenJS.NodeJS.LTS",
			"--accept-source-agreements", "--accept-package-agreements", "--silent"}, RunOptions{})
		if r.Code == 0 {
			pf := os.Getenv("ProgramFiles")
			if pf == "" {
				pf = `C:\Program Files`
			}
			for _, d := range []string{
				filepath.Join(pf, "nodejs"),
				filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "nodejs"),
			} {
				if Exists(filepath.Join(d, "npm.cmd")) {
					PrependProcessPath(d)
				}
			}
			if Which("npm") != "" && Which("npx") != "" {
				return true
			}
		}
		L.Warn("winget install didn't complete — falling back to direct download from nodejs.org")
	}
	return installNodeWindowsZip()
}

// InstallCargo offers to install Rust toolchain for RTK source builds.
func InstallCargo() bool {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return true
	}
	if !Confirm("RTK needs Rust (cargo) to build for your platform. Install it now?", true) {
		return false
	}
	if IsWin {
		ps := "$ErrorActionPreference='Stop'; $u='https://win.rustup.rs/x86_64'; $o=\"$env:TEMP\\rustup-init.exe\"; Invoke-WebRequest -UseBasicParsing -Uri $u -OutFile $o; & $o -y --default-toolchain stable --profile minimal"
		return Run("powershell", []string{"-NoProfile", "-Command", ps}, RunOptions{}).Code == 0
	}
	if Which("curl") == "" && Which("wget") == "" {
		return false
	}
	fetcher := "wget -qO-"
	if Which("curl") != "" {
		fetcher = "curl --proto '=https' --tlsv1.2 -sSf"
	}
	r := Run("sh", []string{"-c",
		fetcher + " https://sh.rustup.rs | sh -s -- -y --default-toolchain stable --profile minimal --no-modify-path"}, RunOptions{})
	if r.Code != 0 {
		return false
	}
	os.Setenv("PATH", os.Getenv("HOME")+"/.cargo/bin:"+os.Getenv("PATH"))
	return Which("cargo") != ""
}
