package util

import (
	"os"
	"strings"
)

// EnsureNodeForTools makes sure npm+npx exist, offering to install Node LTS.
func EnsureNodeForTools() bool {
	if Which("npm") != "" && Which("npx") != "" {
		return true
	}
	L.Warn("CodeGraph and Context-Mode need Node.js (npm/npx), which isn't installed.")
	if !Confirm("Install the latest Node.js LTS now?", true) {
		L.Info("Skipping Node install. Install it later: https://nodejs.org/en/download")
		L.Info("Then re-run: tokless")
		return false
	}
	if installNode() && Which("npm") != "" && Which("npx") != "" {
		L.Ok("Node.js installed.")
		return true
	}
	L.Err("Node install didn't complete. Install manually: https://nodejs.org/en/download")
	return false
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
	if Which("winget") == "" {
		L.Err("winget not found — install Node.js LTS from https://nodejs.org")
		return false
	}
	r := Run("winget", []string{"install", "-e", "--id", "OpenJS.NodeJS.LTS",
		"--accept-source-agreements", "--accept-package-agreements", "--silent"}, RunOptions{})
	return r.Code == 0
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
