package main

import (
	"bytes"
	"os"
	"runtime"
	"testing"
	"time"
)

type fakeConsoleAPI struct {
	handle       uintptr
	handleOK     bool
	mode         uint32
	getModeOK    bool
	setModeOK    bool
	setModeCalls []uint32
}

func (f *fakeConsoleAPI) stdHandle() (uintptr, bool) { return f.handle, f.handleOK }
func (f *fakeConsoleAPI) getMode(uintptr) (uint32, bool) {
	return f.mode, f.getModeOK
}
func (f *fakeConsoleAPI) setMode(_ uintptr, mode uint32) bool {
	f.setModeCalls = append(f.setModeCalls, mode)
	return f.setModeOK
}

func TestEnableConsoleANSIWithRedirectedWriter(t *testing.T) {
	api := &fakeConsoleAPI{handle: 1, handleOK: true, getModeOK: true, setModeOK: true}
	var buf bytes.Buffer
	if enableConsoleANSIWith(&buf, os.Stdout, api) {
		t.Fatal("a redirected writer must not enable ANSI")
	}
	if len(api.setModeCalls) != 0 {
		t.Fatalf("redirected writer must not touch the console mode: %v", api.setModeCalls)
	}
}

func TestEnableConsoleANSIWithFailedStdHandle(t *testing.T) {
	api := &fakeConsoleAPI{handleOK: false}
	if enableConsoleANSIWith(os.Stdout, os.Stdout, api) {
		t.Fatal("a failed GetStdHandle must not enable ANSI")
	}
}

func TestEnableConsoleANSIWithFailedGetMode(t *testing.T) {
	api := &fakeConsoleAPI{handle: 1, handleOK: true, getModeOK: false}
	if enableConsoleANSIWith(os.Stdout, os.Stdout, api) {
		t.Fatal("a failed GetConsoleMode must not enable ANSI")
	}
}

func TestEnableConsoleANSIWithFailedSetMode(t *testing.T) {
	api := &fakeConsoleAPI{handle: 1, handleOK: true, getModeOK: true, setModeOK: false}
	if enableConsoleANSIWith(os.Stdout, os.Stdout, api) {
		t.Fatal("a failed SetConsoleMode must not enable ANSI")
	}
}

func TestEnableConsoleANSIWithSuccess(t *testing.T) {
	api := &fakeConsoleAPI{handle: 1, handleOK: true, mode: 0x1, getModeOK: true, setModeOK: true}
	if !enableConsoleANSIWith(os.Stdout, os.Stdout, api) {
		t.Fatal("a fully successful console call chain should enable ANSI")
	}
	if len(api.setModeCalls) != 1 || api.setModeCalls[0] != 0x1|enableVirtualTerminalProcessing {
		t.Fatalf("expected the existing mode ORed with the VT flag, got %v", api.setModeCalls)
	}
}

func TestDefaultWatchIntervalMatchesDocumentedValue(t *testing.T) {
	// defaultWatchInterval is a compile-time constant selected per GOOS by
	// console_windows.go / console_other.go's build tags; this only checks
	// the branch built for the platform this test runs on.
	want := 2 * time.Second
	if runtime.GOOS == "windows" {
		want = 5 * time.Second
	}
	if defaultWatchInterval != want {
		t.Fatalf("defaultWatchInterval = %v, want %v for %s", defaultWatchInterval, want, runtime.GOOS)
	}
}
