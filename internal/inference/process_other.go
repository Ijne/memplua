//go:build !windows

package inference

import (
	"os"
	"os/exec"
)

type processGuard struct{}

func configureProcess(_ *exec.Cmd) (*processGuard, error)   { return &processGuard{}, nil }
func (*processGuard) attach(*os.Process) error              { return nil }
func (*processGuard) close() error                          { return nil }
func terminateOwnedProcess(int, string) (bool, error)       { return false, nil }
func terminateOwnedProcessOnPort(int, string) (bool, error) { return false, nil }
