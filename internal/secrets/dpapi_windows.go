//go:build windows

package secrets

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Protect cifra un slice de bytes con Windows DPAPI (CryptProtectData)
// usando el alcance del usuario actual (CRYPTPROTECT_UI_FORBIDDEN).
func Protect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	var inBlob windows.DataBlob
	inBlob.Size = uint32(len(data))
	inBlob.Data = &data[0]

	var outBlob windows.DataBlob
	err := windows.CryptProtectData(
		&inBlob,
		nil,
		nil,
		0,
		nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN,
		&outBlob,
	)
	if err != nil {
		return nil, fmt.Errorf("dpapi protect: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))

	out := make([]byte, outBlob.Size)
	copy(out, unsafe.Slice(outBlob.Data, outBlob.Size))
	return out, nil
}

// Unprotect descifra un slice de bytes con Windows DPAPI (CryptUnprotectData).
func Unprotect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	var inBlob windows.DataBlob
	inBlob.Size = uint32(len(data))
	inBlob.Data = &data[0]

	var outBlob windows.DataBlob
	err := windows.CryptUnprotectData(
		&inBlob,
		nil,
		nil,
		0,
		nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN,
		&outBlob,
	)
	if err != nil {
		return nil, fmt.Errorf("dpapi unprotect: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outBlob.Data)))

	out := make([]byte, outBlob.Size)
	copy(out, unsafe.Slice(outBlob.Data, outBlob.Size))
	return out, nil
}
