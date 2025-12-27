package core

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenDir opens the specified directory in the default file manager
func OpenDir(path string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "explorer"
		args = []string{path}
	case "darwin":
		cmd = "open"
		args = []string{path}
	case "linux":
		cmd = "xdg-open"
		args = []string{path}
	default:
		return fmt.Errorf("unsupported platform")
	}

	return exec.Command(cmd, args...).Start()
}
