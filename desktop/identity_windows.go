package desktop

import (
	"fmt"
	"syscall"
	"unsafe"
)

// SetApplicationIdentity groups the process's windows with the installed app.
func SetApplicationIdentity(id string) error {
	if id == "" {
		return nil
	}
	name, err := syscall.UTF16PtrFromString(id)
	if err != nil {
		return err
	}
	setID := syscall.NewLazyDLL("shell32.dll").NewProc("SetCurrentProcessExplicitAppUserModelID")
	result, _, _ := setID.Call(uintptr(unsafe.Pointer(name)))
	if int32(result) < 0 {
		return fmt.Errorf("set Windows application ID: HRESULT %#x", uint32(result))
	}
	return nil
}
