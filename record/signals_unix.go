//go:build unix

package record

import (
	"os"
	"syscall"
)

func stopSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
