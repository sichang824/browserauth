//go:build windows

package record

import "os"

func stopSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
