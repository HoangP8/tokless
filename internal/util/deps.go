package util

// RuntimeReport lists discovered package runtimes (path or "").
type RuntimeReport struct {
	Node, Npm, Npx, Bun, Bunx, Cargo, Python string
}

func DetectRuntimes() RuntimeReport {
	py := Which("python3")
	if py == "" {
		py = Which("python")
	}
	return RuntimeReport{
		Node:   Which("node"),
		Npm:    Which("npm"),
		Npx:    Which("npx"),
		Bun:    Which("bun"),
		Bunx:   Which("bunx"),
		Cargo:  Which("cargo"),
		Python: py,
	}
}

// PickJsRunner prefers npx, then bunx.
func PickJsRunner() (cmd string, runArgs []string, ok bool) {
	if Which("npx") != "" {
		return "npx", []string{"-y"}, true
	}
	if Which("bunx") != "" {
		return "bunx", []string{}, true
	}
	return "", nil, false
}

func ReportRuntimes(r RuntimeReport) {
	fmtLine := func(name, p string) {
		if p != "" {
			L.Sub(name + ": " + p)
		} else {
			L.Sub(name + ": not found")
		}
	}
	fmtLine("node", r.Node)
	npmnpx := r.Npm
	if npmnpx == "" {
		npmnpx = r.Npx
	}
	fmtLine("npm/npx", npmnpx)
	bunbunx := r.Bun
	if bunbunx == "" {
		bunbunx = r.Bunx
	}
	fmtLine("bun/bunx", bunbunx)
	fmtLine("cargo", r.Cargo)
	fmtLine("python", r.Python)
}
