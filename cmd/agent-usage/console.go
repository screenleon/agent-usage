package main

import "io"

// consoleAPI abstracts the Win32 console calls enableConsoleANSI needs, so
// its decision logic can be unit-tested on any platform.
type consoleAPI interface {
	stdHandle() (handle uintptr, ok bool)
	getMode(handle uintptr) (mode uint32, ok bool)
	setMode(handle uintptr, mode uint32) bool
}

// enableConsoleANSIWith enables VT sequences in legacy Windows consoles. When
// stdout is redirected or a policy disallows it, watch falls back to a plain
// newline between snapshots instead of printing raw escape characters.
func enableConsoleANSIWith(w, stdout io.Writer, api consoleAPI) bool {
	if w != stdout {
		return false
	}
	handle, ok := api.stdHandle()
	if !ok {
		return false
	}
	mode, ok := api.getMode(handle)
	if !ok {
		return false
	}
	return api.setMode(handle, mode|enableVirtualTerminalProcessing)
}

const enableVirtualTerminalProcessing = 0x0004
