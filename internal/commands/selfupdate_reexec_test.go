package commands

import (
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

func TestWindowsSelfUpdateEnvIncludesPIDHandoff(t *testing.T) {
	got := strings.Join(windowsSelfUpdateEnv(1234, `C:\\Users\\test\\tokless.exe`), ",")
	for _, want := range []string{"CI=1", "TOKLESS_SELF_UPDATE_PID=1234", `TOKLESS_SELF_UPDATE_PATH=C:\\Users\\test\\tokless.exe`} {
		if !strings.Contains(got, want) {
			t.Errorf("Windows update env missing %q: %s", want, got)
		}
	}
}

func TestWindowsUpdateInstallerStagesAfterDownload(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"TOKLESS_SELF_UPDATE_PID", "TOKLESS_SELF_UPDATE_PATH", "$process.WaitForExit()", "[System.IO.File]::Replace($staged, $Destination, $null)", "if (Test-Path -LiteralPath $Destination)", "Move-Item -LiteralPath $staged -Destination $Destination", "StatusFile", "Update replacement completed", "--version", "$dataRoot = if ($env:LOCALAPPDATA) { $env:LOCALAPPDATA } else { Join-Path $env:USERPROFILE \"AppData\\Local\" }", "Join-Path $dataRoot \"tokless\"", "path    = $Destination", "version = $version", "at      = (Get-Date).ToUniversalTime()", "Update replacement failed", "$helperArgs", "ConvertFrom-Json", "Process ID must be a positive integer", "Remove-Item -LiteralPath $staged", "ProcessId = $selfUpdate", "StartTime = $targetStartTime", "Path = $targetPath", "Get-Process -Id $ProcessId -ErrorAction Stop", "Self-update process identity changed", "$process.WaitForExit()", "finally {", "status write failed", "Remove-Item -LiteralPath $ConfigFile", "Update scheduled; replacement runs after tokless exits; run tokless again", "Download failed ($asset): $($_.Exception.ToString())", "$PSVersionTable.PSVersion.Major -eq 5", "$PSVersionTable.PSVersion.Minor -eq 1", "SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12", "Replacement failed"} {
		if !strings.Contains(s, want) {
			t.Errorf("install.ps1 missing %q", want)
		}
	}
	replacement := strings.Index(s, "[System.IO.File]::Replace($staged, $Destination, $null)")
	versionLookup := strings.Index(s, "$versionOutput = & $Destination --version")
	markerWrite := strings.Index(s, `method  = "install script"`)
	if replacement < 0 || versionLookup < 0 || markerWrite < 0 || replacement >= versionLookup || versionLookup >= markerWrite {
		t.Fatalf("marker refresh must follow replacement: replacement=%d version=%d marker=%d", replacement, versionLookup, markerWrite)
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
