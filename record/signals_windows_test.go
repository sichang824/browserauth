//go:build windows

package record

import (
	"os"
	"testing"
)

func TestStopSignalsIncludeInterrupt(t *testing.T) {
	sigs := stopSignals()
	if !hasSignal(sigs, os.Interrupt) {
		t.Fatalf("signals = %v", sigs)
	}
}

func hasSignal(sigs []os.Signal, want os.Signal) bool {
	for _, sig := range sigs {
		if sig == want {
			return true
		}
	}
	return false
}
