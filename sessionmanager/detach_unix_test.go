//go:build unix

package sessionmanager

import (
	"os/exec"
	"testing"
)

func TestDetachProcessStartsNewSession(t *testing.T) {
	cmd := exec.Command("true")
	detachProcess(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Fatal("session manager child must start in a new session")
	}
}
