//go:build !windows

package lock

import (
	"os"
	"syscall"
)

// unixPidAlive usa señal 0: no mata, solo chequea existencia.

func unixPidAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
