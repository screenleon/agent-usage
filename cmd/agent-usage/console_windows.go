//go:build windows

package main

import (
	"io"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const stdOutputHandle = ^uintptr(10) // STD_OUTPUT_HANDLE (-11)

const defaultWatchInterval = 5 * time.Second

func enableConsoleANSI(w io.Writer) bool {
	return enableConsoleANSIWith(w, os.Stdout, kernel32Console{})
}

type kernel32Console struct{}

func (kernel32Console) stdHandle() (uintptr, bool) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	handle, _, _ := kernel32.NewProc("GetStdHandle").Call(stdOutputHandle)
	if handle == 0 || handle == ^uintptr(0) {
		return 0, false
	}
	return handle, true
}

func (kernel32Console) getMode(handle uintptr) (uint32, bool) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	var mode uint32
	ok, _, _ := kernel32.NewProc("GetConsoleMode").Call(handle, uintptr(unsafe.Pointer(&mode)))
	return mode, ok != 0
}

func (kernel32Console) setMode(handle uintptr, mode uint32) bool {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	ok, _, _ := kernel32.NewProc("SetConsoleMode").Call(handle, uintptr(mode))
	return ok != 0
}
