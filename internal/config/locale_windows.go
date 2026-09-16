//go:build windows

package config

import (
	"strings"
	"syscall"
	"unsafe"
)

func defaultUILanguage() string {
	var locale [85]uint16
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	count, _, _ := proc.Call(uintptr(unsafe.Pointer(&locale[0])), uintptr(len(locale)))
	if count > 0 && strings.HasPrefix(strings.ToLower(syscall.UTF16ToString(locale[:])), "ru") {
		return "ru"
	}
	return "en"
}
