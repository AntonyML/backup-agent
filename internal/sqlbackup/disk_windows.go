//go:build windows

package sqlbackup

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	modKernel32            = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceEx = modKernel32.NewProc("GetDiskFreeSpaceExW")
)

// FreeBytes devuelve los bytes libres disponibles para el usuario actual
// en el volumen que contiene path (requiere que path exista).
func FreeBytes(path string) (int64, error) {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeToCaller, total, free int64
	r1, _, e := syscall.SyscallN(procGetDiskFreeSpaceEx.Addr(),
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&free)))
	if r1 == 0 {
		if e != 0 {
			return 0, e
		}
		return 0, fmt.Errorf("GetDiskFreeSpaceExW falló para %s", path)
	}
	return freeToCaller, nil
}
