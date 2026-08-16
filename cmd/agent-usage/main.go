package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/screenleon/agent-usage/internal/collect"
	"github.com/screenleon/agent-usage/internal/quota"
	"github.com/screenleon/agent-usage/internal/render"
)

const usageText = `Usage: agent-usage [watch|-w [N]] [--recent] [--offline] [--json] [-h]

  watch / -w [N]  keep refreshing (default 2s). Ctrl-C to stop
  --recent        include sessions updated in the last 2 hours
  --offline       skip Grok/Codex quota HTTP fetches
  --json          machine-readable snapshot
  -h              help

Reads live process + tiny session metadata only. Does not scan transcripts.
`

func main() {
	var (
		watch    bool
		interval = 2 * time.Second
		recent   bool
		offline  bool
		asJSON   bool
	)
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "watch" {
		watch = true
		args = args[1:]
		if len(args) > 0 {
			if n, err := strconv.Atoi(args[0]); err == nil {
				if n < 1 {
					n = 1
				}
				interval = time.Duration(n) * time.Second
				args = args[1:]
			}
		}
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-w", "--watch":
			watch = true
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil {
					if n < 1 {
						n = 1
					}
					interval = time.Duration(n) * time.Second
					i++
				}
			}
		case "--recent", "--all":
			recent = true
		case "--offline":
			offline = true
		case "--json":
			asJSON = true
		case "-h", "--help":
			fmt.Fprint(os.Stdout, usageText)
			return
		default:
			fmt.Fprintf(os.Stderr, "agent-usage: unknown argument: %s\n%s", args[i], usageText)
			os.Exit(2)
		}
	}

	home, _ := os.UserHomeDir()
	opt := collect.Options{Home: home, Recent: recent}

	printOnce := func() {
		snap := collect.Collect(opt)
		var q *quota.Report
		if !offline {
			rep := quota.Load(quota.Options{Home: home})
			q = &rep
		}
		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(struct {
				collect.Snapshot
				Quota *quota.Report `json:"quota,omitempty"`
			}{snap, q})
			return
		}
		var iv time.Duration
		if watch {
			iv = interval
		}
		render.Snapshot(os.Stdout, snap, q, iv)
	}

	if !watch {
		printOnce()
		return
	}

	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	fmt.Print("\033[H\033[J")
	printOnce()
	for {
		select {
		case <-ch:
			fmt.Println()
			return
		case <-tick.C:
			fmt.Print("\033[H\033[J")
			printOnce()
		}
	}
}
