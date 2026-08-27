package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/screenleon/agent-usage/internal/collect"
	"github.com/screenleon/agent-usage/internal/quota"
	"github.com/screenleon/agent-usage/internal/render"
)

const usageText = `Usage: agent-usage [watch|-w [N]] [--recent] [--offline] [--json]
                   [--agent a,b] [--fail-under N] [-h]

  watch / -w [N]  keep refreshing (default 2s). Ctrl-C to stop
  --recent        include idle sessions updated in the last 2 hours
  --offline       skip Grok/Codex quota HTTP fetches
  --json          machine-readable snapshot
  --agent a,b     only these agents (claude, grok, codex, opencode)
  --fail-under N  exit 1 if any shown quota remaining is below N percent
  -h              help

Reads live process + tiny session metadata only. Does not scan transcripts.
`

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent-usage: %v\n%s", err, usageText)
		os.Exit(2)
	}
	if cfg.help {
		fmt.Fprint(os.Stdout, usageText)
		return
	}

	home, _ := os.UserHomeDir()
	opt := collect.Options{Home: home, Recent: cfg.recent, Agents: cfg.agents}

	printOnce := func() int {
		snap := collect.Collect(opt)
		var q *quota.Report
		if !cfg.offline {
			rep := quota.Load(quota.Options{Home: home, Agents: cfg.agents})
			q = &rep
		}
		if cfg.asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(struct {
				collect.Snapshot
				Quota *quota.Report `json:"quota,omitempty"`
			}{snap, q})
		} else {
			var iv time.Duration
			if cfg.watch {
				iv = cfg.interval
			}
			render.Snapshot(os.Stdout, snap, q, iv)
		}
		if cfg.failUnder != nil && q != nil {
			if min, ok := q.MinRemaining(nil); ok && min < *cfg.failUnder {
				return 1
			}
		}
		return 0
	}

	if !cfg.watch {
		os.Exit(printOnce())
		return
	}

	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	tick := time.NewTicker(cfg.interval)
	defer tick.Stop()
	run := func() {
		fmt.Print("\033[H\033[J")
		if code := printOnce(); code != 0 && cfg.failUnder != nil {
			os.Exit(code)
		}
	}
	run()
	for {
		select {
		case <-ch:
			fmt.Println()
			return
		case <-tick.C:
			run()
		}
	}
}

type config struct {
	watch     bool
	interval  time.Duration
	recent    bool
	offline   bool
	asJSON    bool
	help      bool
	agents    []string
	failUnder *float64
}

func parseArgs(args []string) (config, error) {
	cfg := config{interval: 2 * time.Second}
	if len(args) > 0 && args[0] == "watch" {
		cfg.watch = true
		args = args[1:]
		if len(args) > 0 && setWatchInterval(&cfg, args[0]) {
			args = args[1:]
		}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-w" || a == "--watch":
			cfg.watch = true
			if i+1 < len(args) && setWatchInterval(&cfg, args[i+1]) {
				i++
			}
		case a == "--recent" || a == "--all":
			cfg.recent = true
		case a == "--offline":
			cfg.offline = true
		case a == "--json":
			cfg.asJSON = true
		case a == "-h" || a == "--help":
			cfg.help = true
		case a == "--agent" || strings.HasPrefix(a, "--agent="):
			val, next, err := flagValue(a, "--agent", args, i)
			if err != nil {
				return cfg, err
			}
			i = next
			agents, err := parseAgents(val)
			if err != nil {
				return cfg, err
			}
			cfg.agents = agents
		case a == "--fail-under" || strings.HasPrefix(a, "--fail-under="):
			val, next, err := flagValue(a, "--fail-under", args, i)
			if err != nil {
				return cfg, err
			}
			i = next
			n, err := strconv.ParseFloat(val, 64)
			if err != nil || n < 0 {
				return cfg, fmt.Errorf("invalid --fail-under %q", val)
			}
			cfg.failUnder = &n
		default:
			return cfg, fmt.Errorf("unknown argument: %s", a)
		}
	}
	return cfg, nil
}

func setWatchInterval(cfg *config, s string) bool {
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	if n < 1 {
		n = 1
	}
	cfg.interval = time.Duration(n) * time.Second
	return true
}

func flagValue(arg, name string, args []string, i int) (string, int, error) {
	pre := name + "="
	if strings.HasPrefix(arg, pre) {
		v := arg[len(pre):]
		if v == "" {
			return "", i, fmt.Errorf("%s requires a value", name)
		}
		return v, i, nil
	}
	if i+1 >= len(args) {
		return "", i, fmt.Errorf("%s requires a value", name)
	}
	return args[i+1], i + 1, nil
}

func parseAgents(s string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		switch p {
		case "claude", "grok", "codex", "opencode":
		default:
			return nil, fmt.Errorf("unknown agent %q", p)
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--agent requires a value")
	}
	return out, nil
}
