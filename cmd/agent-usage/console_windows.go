//go:build windows

package main

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

const (
	stdOutputHandle                 = ^uintptr(10) // STD_OUTPUT_HANDLE (-11)
	enableVirtualTerminalProcessing = 0x0004
)

// enableConsoleANSI enables VT sequences in legacy Windows consoles. When
// stdout is redirected or a policy disallows it, watch falls back to a plain
// newline between snapshots instead of printing raw escape characters.
func enableConsoleANSI(w io.Writer) bool {
	if w != os.Stdout {
		return false
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	handle, _, _ := kernel32.NewProc("GetStdHandle").Call(stdOutputHandle)
	if handle == 0 || handle == ^uintptr(0) {
		return false
	}
	var mode uint32
	getMode := kernel32.NewProc("GetConsoleMode")
	if ok, _, _ := getMode.Call(handle, uintptr(unsafe.Pointer(&mode))); ok == 0 {
		return false
	}
	setMode := kernel32.NewProc("SetConsoleMode")
	ok, _, _ := setMode.Call(handle, uintptr(mode|enableVirtualTerminalProcessing))
	return ok != 0
}
