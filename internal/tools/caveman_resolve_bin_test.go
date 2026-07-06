package tools

import (
	"testing"
)

// TestResolveSkillsBinStripsPrefix proves the hardcoded npxArgs[2:] slice
// strips exactly the ["-y","skills"] prefix produced by cavemanSkillsAddArgs.
func TestResolveSkillsBinStripsPrefix(t *testing.T) {
	addArgs := cavemanSkillsAddArgs("codex")
	if addArgs[0] != "-y" || addArgs[1] != "skills" {
		t.Fatalf("cavemanSkillsAddArgs must keep -y skills prefix, got %v", addArgs)
	}

	bin, args := resolveSkillsBin(addArgs)
	switch bin {
	case "skills":
		if len(args) != len(addArgs)-2 {
			t.Fatalf("skills path must drop 2 prefix args: got %d from %d", len(args), len(addArgs))
		}
		if args[0] != "add" || args[1] != "JuliusBrussee/caveman" {
			t.Fatalf("skills args wrong after strip: %v", args)
		}
	case "npx":
		if len(args) != len(addArgs) {
			t.Fatalf("npx path must keep all args: got %d want %d", len(args), len(addArgs))
		}
		if args[0] != "-y" || args[1] != "skills" {
			t.Fatalf("npx fallback must keep -y skills prefix, got %v", args)
		}
	default:
		t.Fatalf("unexpected bin %q", bin)
	}
}

// TestCavemanSkillsRemoveArgsPrefixSharesShape pins that remove args share
// the ["-y","skills"] prefix with add args.
func TestCavemanSkillsRemoveArgsPrefixSharesShape(t *testing.T) {
	args := cavemanSkillsRemoveArgs("codex")
	if len(args) < 2 || args[0] != "-y" || args[1] != "skills" {
		t.Fatalf("remove args must keep -y skills prefix for resolveSkillsBin, got %v", args)
	}
	stripped := args[2:]
	if stripped[0] != "remove" {
		t.Fatalf("after strip, first arg must be remove, got %v", stripped)
	}
}

// TestResolveCavemanBinShape pins the caveman-vs-npx arg shape so a future
// edit can't silently swap flags.
func TestResolveCavemanBinShape(t *testing.T) {
	bin, args := resolveCavemanBin("opencode", false)
	switch bin {
	case "caveman":
		if args[0] != "--only" || args[1] != "opencode" || args[2] != "--no-mcp-shrink" {
			t.Fatalf("caveman bin args wrong: %v", args)
		}
		for _, a := range args {
			if a == "--force" {
				t.Fatalf("non-upgrade must NOT have --force: %v", args)
			}
		}
	case "npx":
		if args[0] != "-y" || args[1] != "github:JuliusBrussee/caveman" || args[2] != "--" {
			t.Fatalf("npx caveman args wrong: %v", args)
		}
		for _, a := range args {
			if a == "--force" {
				t.Fatalf("non-upgrade must NOT have --force: %v", args)
			}
		}
	default:
		t.Fatalf("unexpected bin %q", bin)
	}

	bin, args = resolveCavemanBin("codex", true)
	switch bin {
	case "caveman":
		found := false
		for _, a := range args {
			if a == "--force" {
				found = true
			}
		}
		if !found {
			t.Fatalf("caveman upgrade=true must include --force, got %v", args)
		}
	case "npx":
		found := false
		for _, a := range args {
			if a == "--force" {
				found = true
			}
		}
		if !found {
			t.Fatalf("npx upgrade=true must include --force, got %v", args)
		}
	default:
		t.Fatalf("unexpected bin %q", bin)
	}
}