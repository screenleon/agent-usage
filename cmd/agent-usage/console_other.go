//go:build !windows

package main

import "io"

func enableConsoleANSI(io.Writer) bool { return true }
