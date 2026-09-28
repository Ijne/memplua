package main

import (
	"syscall"
	"unsafe"
)

// Configuration can fail before the desktop host and its logger exist.
// A GUI-subsystem release must not hide that error in an invisible stderr.
func notifyStartupFailure(err error) {
	message, _ := syscall.UTF16PtrFromString("memplua could not start:\n\n" + err.Error())
	title, _ := syscall.UTF16PtrFromString("memplua — startup error")
	_, _, _ = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(
		0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
}
