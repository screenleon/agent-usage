//go:build !windows

package main

import (
	"io"
	"time"
)

const defaultWatchInterval = 2 * time.Second

func enableConsoleANSI(io.Writer) bool { return true }
