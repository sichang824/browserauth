//go:build windows

package sessionmanager

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestDetachProcessStartsNewProcessGroup(t *testing.T) {
	cmd := exec.Command("true")
	detachProcess(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatal("session manager child must start in a new process group")
	}
}
