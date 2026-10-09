//go:build unix

package cmd

import (
	"os"
	"syscall"
)

func stopSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
