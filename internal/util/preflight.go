package util

import (
	"os"
	"path/filepath"
	"strings"
)

// EnsureDeps detects all missing external deps up front and offers one
// combined install.
func EnsureDeps(needNode, needGit bool) (nodeOK, gitOK bool) {
	nodeOK = !needNode || nodeToolsReady()
	gitOK = !needGit || Which("git") != ""
	if nodeOK && gitOK {
		return
	}
	var missing []string
	if !nodeOK {
		missing = append(missing, "Node.js (CodeGraph, Context-Mode)")
	}
	if !gitOK {
		missing = append(missing, "git (Caveman)")
	}
	L.Warn("Missing: " + strings.Join(missing, ", "))
	if !Confirm("Install now?", true) {
		L.Info("Skipping. Node: https://nodejs.org/en/download · git: https://git-scm.com/downloads")
		return
	}
	if !nodeOK {
		if installNode() && nodeToolsReady() {
			L.Ok("Node.js installed.")
			nodeOK = true
		} else {
			L.Err("Node install didn't complete. Manual: https://nodejs.org/en/download")
		}
	}
	if !gitOK {
		if installGit() && Which("git") != "" {
			L.Ok("Git installed.")
			gitOK = true
		} else {
			L.Err("Git install didn't complete. Manual: https://git-scm.com/downloads")
		}
	}
	return
}

func installGit() bool {
	if os.Getenv("TOKLESS_TEST") == "1" {
		return true
	}
	if IsWin {
		return installGitWindowsZip()
	}
	L.Info("Install git with your package manager (apt/dnf/brew), then re-run: tokless")
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
	return installNodeUnixTarball()
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
