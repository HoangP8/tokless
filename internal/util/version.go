package util

// Version is injected at build time via -ldflags "-X .../util.Version=x.y.z".
var Version string

func ToklessVersion() string {
	if Version != "" {
		return Version
	}
	return "0.0.0-dev"
}
