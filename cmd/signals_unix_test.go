//go:build unix

package cmd

import (
	"os"
	"syscall"
	"testing"
)

func TestStopSignalsIncludeInterruptAndTerm(t *testing.T) {
	sigs := stopSignals()
	if !hasSignal(sigs, os.Interrupt) || !hasSignal(sigs, syscall.SIGTERM) {
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
