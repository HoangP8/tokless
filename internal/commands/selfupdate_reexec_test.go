package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestReexecSelfImplHandoff: real process handoff on every OS.
// Outer spawns probe; probe reexecs once; reexeced process prints AFTER_REEXEC.
func TestReexecSelfImplHandoff(t *testing.T) {
	if os.Getenv("TOKLESS_REEXEC_PROBE") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=TestReexecSelfImplHandoff$", "--")
		cmd.Env = append(os.Environ(), "TOKLESS_REEXEC_PROBE=1")
		cmd.Env = withEnvKV(cmd.Env, reexecEnvKey, "")
		env := make([]string, 0, len(cmd.Env))
		for _, e := range cmd.Env {
			if strings.HasPrefix(e, reexecEnvKey+"=") {
				continue
			}
			env = append(env, e)
		}
		cmd.Env = append(env, "TOKLESS_REEXEC_PROBE=1")
		out, err := cmd.CombinedOutput()
		s := string(out)
		if err != nil {
			t.Fatalf("probe failed: %v\n%s", err, s)
		}
		if !strings.Contains(s, "BEFORE_REEXEC") {
			t.Fatalf("missing BEFORE_REEXEC:\n%s", s)
		}
		if !strings.Contains(s, "AFTER_REEXEC") {
			t.Fatalf("missing AFTER_REEXEC (handoff failed):\n%s", s)
		}
		if strings.Index(s, "AFTER_REEXEC") < strings.Index(s, "BEFORE_REEXEC") {
			t.Fatalf("order wrong:\n%s", s)
		}
		return
	}

	if os.Getenv(reexecEnvKey) == "1" {
		os.Stdout.Write([]byte("AFTER_REEXEC\n"))
		os.Exit(0)
	}
	os.Stdout.Write([]byte("BEFORE_REEXEC\n"))
	if err := reexecSelfImpl(); err != nil {
		t.Fatalf("reexecSelfImpl: %v", err)
	}
	t.Fatal("reexecSelfImpl returned without handoff")
}

// TestSelfUpdateBinarySwapThenReexec: old binary overwrites itself with new,
// reexecs, continued process must print NEW_MARKER.
func TestSelfUpdateBinarySwapThenReexec(t *testing.T) {
	if testing.Short() {
		t.Skip("builds real binaries")
	}
	if runtime.GOOS == "windows" {
		t.Skip("rename-over-running-exe not reliable on Windows; handoff covered by TestReexecSelfImplHandoff")
	}

	dir := t.TempDir()
	oldSrc := filepath.Join(dir, "old")
	newSrc := filepath.Join(dir, "new")
	for _, d := range []string{oldSrc, newSrc} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	oldMain := `package main
import (
  "fmt"
  "io"
  "os"
  "path/filepath"
  "syscall"
)
const marker = "OLD_MARKER"
func main() {
  if os.Getenv("TOKLESS_REEXECED") == "1" {
    fmt.Println(marker)
    return
  }
  exe, err := os.Executable()
  if err != nil { panic(err) }
  if r, err := filepath.EvalSymlinks(exe); err == nil && r != "" { exe = r }
  in, err := os.Open(filepath.Join(filepath.Dir(exe), "newbin"))
  if err != nil { panic(err) }
  defer in.Close()
  tmp := exe + ".tmp"
  out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
  if err != nil { panic(err) }
  if _, err := io.Copy(out, in); err != nil { panic(err) }
  out.Close()
  if err := os.Rename(tmp, exe); err != nil { panic(err) }
  fmt.Println("SWAPPED")
  env := append(os.Environ(), "TOKLESS_REEXECED=1")
  if err := syscall.Exec(exe, []string{exe}, env); err != nil { panic(err) }
}
`
	newMain := `package main
import (
  "fmt"
  "os"
)
const marker = "NEW_MARKER"
func main() {
  if os.Getenv("TOKLESS_REEXECED") == "1" {
    fmt.Println(marker)
    return
  }
  fmt.Println("NEW_WITHOUT_REEXEC")
}
`
	writeProg := func(srcDir, main string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(main), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module tmp\n\ngo 1.22\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeProg(oldSrc, oldMain)
	writeProg(newSrc, newMain)

	oldBin := filepath.Join(dir, "tokless-old")
	newBin := filepath.Join(dir, "newbin")
	build := func(srcDir, out string) {
		t.Helper()
		cmd := exec.Command("go", "build", "-o", out, ".")
		cmd.Dir = srcDir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", srcDir, err, b)
		}
	}
	build(oldSrc, oldBin)
	build(newSrc, newBin)

	cmd := exec.Command(oldBin)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	s := string(out)
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, s)
	}
	if !strings.Contains(s, "SWAPPED") {
		t.Fatalf("did not swap:\n%s", s)
	}
	if !strings.Contains(s, "NEW_MARKER") {
		t.Fatalf("want NEW_MARKER after reexec:\n%s", s)
	}
	if strings.Contains(s, "OLD_MARKER") {
		t.Fatalf("still OLD_MARKER after reexec:\n%s", s)
	}
}

func TestWindowsUpdateInstallerStagesAfterDownload(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"TOKLESS_SELF_UPDATE_PATH",
		`$tmp = "$dest.download"`,
		`$old = "$dest.old"`,
		"Get-FileHash -LiteralPath $tmp -Algorithm SHA256",
		"SHA256SUMS",
		"Checksum mismatch",
		"Move-Item -LiteralPath $dest -Destination $old -Force",
		"Move-Item -LiteralPath $tmp -Destination $dest -Force",
		"Restore-Previous",
		"Previous tokless.exe restored.",
		"Get-InstalledVersion",
		"FileVersionInfo",
		`$dataRoot = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $env:USERPROFILE "AppData\Local" }`,
		`Join-Path $dataRoot "tokless"`,
		"path    = $dest",
		"version = $Version",
		"at      = (Get-Date).ToUniversalTime()",
		`method  = "install script"`,
		`Download failed ($asset): $($_.Exception.Message)`,
		"$PSVersionTable.PSVersion.Major -eq 5",
		"$PSVersionTable.PSVersion.Minor -eq 1",
		"SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12",
		`Replacement failed: $($_.Exception.Message)`,
		`-Filter ((Split-Path -Leaf $dest) + ".old*")`,
		`-Filter ((Split-Path -Leaf $dest) + ".*.bak")`,
		"https://www.microsoft.com/en-us/wdsi/filesubmission",
		"Update installed: $v",
		"Run tokless again to use the new version.",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("install.ps1 missing %q", want)
		}
	}
	for _, gone := range []string{
		"windows-defender.md",
		"WindowStyle Hidden",
		"ConvertFrom-Json",
		"WaitForExit",
		"ToFileTimeUtc",
		"Self-update process identity changed",
		"tokless-update-status-",
		"[System.IO.File]::Replace",
		`Get-Process -Id $ProcessId`,
		"StatusFile",
		"$helperArgs",
		"$ConfigFile",
		"$staged",
		"$Destination",
		"--version",
		"NewGuid",
	} {
		if strings.Contains(s, gone) {
			t.Errorf("install.ps1 must not contain %q", gone)
		}
	}
	swap := strings.Index(s, "Move-Item -LiteralPath $tmp -Destination $dest -Force")
	version := strings.Index(s, "$v = Get-InstalledVersion $dest")
	marker := strings.Index(s, "Write-InstallMarker $v")
	if swap < 0 || version < 0 || marker < 0 || swap >= version || version >= marker {
		t.Fatalf("marker refresh must follow swap and version check: swap=%d version=%d marker=%d", swap, version, marker)
	}
}

func TestWithEnvKV(t *testing.T) {
	got := withEnvKV([]string{"A=1", "B=2"}, "B", "9")
	if strings.Join(got, ",") != "A=1,B=9" {
		t.Fatalf("replace: %v", got)
	}
	got = withEnvKV([]string{"A=1"}, "C", "3")
	if strings.Join(got, ",") != "A=1,C=3" {
		t.Fatalf("append: %v", got)
	}
}

func TestReexecAfterSelfUpdateSkipsWhenGuarded(t *testing.T) {
	t.Setenv(reexecEnvKey, "1")
	// must not hang or exit
	reexecAfterSelfUpdate()
	t.Setenv(reexecEnvKey, "")
	t.Setenv("TOKLESS_TEST", "1")
	reexecAfterSelfUpdate()
}

func TestSwapBinaryReplacesTarget(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tokless.exe")
	download := exe + ".download"
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(download, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := swapBinary(exe, download); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("exe = %q, want new", got)
	}
	if _, err := os.Stat(download); !os.IsNotExist(err) {
		t.Fatalf("download should be consumed, stat err = %v", err)
	}
}

func TestSwapBinaryRollsBackOnFailure(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tokless.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := swapBinary(exe, exe+".download"); err == nil {
		t.Fatal("want error for missing download")
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("exe not restored: %v", err)
	}
	if string(got) != "old" {
		t.Fatalf("exe = %q, want old", got)
	}
}

func TestChecksumFor(t *testing.T) {
	const sums = "aaaa  tokless-linux-x64\nb697  tokless-windows-x64.exe\n"
	if got, ok := checksumFor(sums, "tokless-windows-x64.exe"); !ok || got != "b697" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if _, ok := checksumFor(sums, "tokless-windows-arm64.exe"); ok {
		t.Fatal("unexpected match")
	}
}

func TestRestorePrevious(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tokless.exe")
	old := fmt.Sprintf("%s.old.%d", exe, os.Getpid())
	if err := os.WriteFile(old, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := restorePrevious(exe); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "old" {
		t.Fatalf("restored %q, err %v", got, err)
	}
}

// TestSwapBinaryKeepsExecutablePath: the swap must not move the running image aside.
func TestSwapBinaryKeepsExecutablePath(t *testing.T) {
	if os.Getenv("TOKLESS_SWAP_PROBE") == "1" {
		exe, err := resolvedExecutable()
		if err != nil {
			fmt.Println("PROBE_EXECUTABLE_ERR", err)
			os.Exit(11)
		}
		if err := os.WriteFile(exe+".download", []byte("new-content"), 0o755); err != nil {
			fmt.Println("PROBE_WRITE_ERR", err)
			os.Exit(12)
		}
		if err := swapBinary(exe, exe+".download"); err != nil {
			fmt.Println("PROBE_SWAP_ERR", err)
			os.Exit(13)
		}
		got, err := resolvedExecutable()
		if err != nil || filepath.Clean(got) != filepath.Clean(exe) {
			fmt.Println("PROBE_PATH_MISMATCH", got, err)
			os.Exit(14)
		}
		if b, err := os.ReadFile(got); err != nil || string(b) != "new-content" {
			fmt.Println("PROBE_CONTENT_MISMATCH", err)
			os.Exit(15)
		}
		fmt.Println("PROBE_OK")
		os.Exit(0)
	}

	src, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(t.TempDir(), "probe"+filepath.Ext(src))
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(probe, data, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(probe, "-test.run=TestSwapBinaryKeepsExecutablePath$")
	cmd.Env = append(os.Environ(), "TOKLESS_SWAP_PROBE=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PROBE_OK") {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
}

func TestRestorePreviousPlainOld(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tokless.exe")
	if err := os.WriteFile(exe+".old", []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := restorePrevious(exe); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(exe); err != nil || string(got) != "old" {
		t.Fatalf("restored %q, err %v", got, err)
	}
}

func TestVerifySHA256(t *testing.T) {
	p := filepath.Join(t.TempDir(), "blob")
	if err := os.WriteFile(p, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	const sum = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if err := verifySHA256(p, sum); err != nil {
		t.Fatal(err)
	}
	if err := verifySHA256(p, strings.Repeat("0", 64)); err == nil {
		t.Fatal("want checksum mismatch")
	}
}
