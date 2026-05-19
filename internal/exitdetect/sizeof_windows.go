//go:build windows

package exitdetect

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Process32First/Next requires Size = sizeof(PROCESSENTRY32W). Exposing
// unsafe.Sizeof from a const context isn't possible, so wrap it.
func unsafe_Sizeof_ProcessEntry32() uintptr {
	return unsafe.Sizeof(windows.ProcessEntry32{})
}
