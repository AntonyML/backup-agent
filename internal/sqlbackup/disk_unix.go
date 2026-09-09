//go:build !windows

package sqlbackup

import "syscall"

// FreeBytes devuelve los bytes libres disponibles (Bavail) en el volumen
// que contiene path (requiere que path exista).
func FreeBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
