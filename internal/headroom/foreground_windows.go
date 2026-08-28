//go:build windows

package headroom

func runHeadroomForeground(bin string, args []string) error {
	return runHeadroomSupervised(bin, args)
}
