package util

import "os"

// headroom's proxy runs as a background service it installs itself. tokless
// only asks whether it's there and tells it to restart.

func HeadroomProxyDeployed() bool {
	hr := ResolvePyBin("headroom")
	if hr == "" || os.Getenv("TOKLESS_TEST") == "1" {
		return false
	}
	return Run(hr, []string{"install", "status"}, RunOptions{Capture: true, Quiet: true}).Code == 0
}

func HeadroomProxyRestart() {
	if hr := ResolvePyBin("headroom"); hr != "" {
		Run(hr, []string{"install", "restart"}, RunOptions{Capture: true, Quiet: true})
	}
}

func HeadroomProxyRemove() {
	if hr := ResolvePyBin("headroom"); hr != "" {
		Run(hr, []string{"install", "remove"}, RunOptions{Capture: true})
	}
}
