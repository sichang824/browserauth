//go:build windows

package cmd

import "os"

func stopSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
