package main

import (
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	cfg, err := parseArgs([]string{"watch", "3", "--recent", "--agent", "claude,codex", "--fail-under", "15"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.watch || cfg.interval.Seconds() != 3 || !cfg.recent || cfg.failUnder == nil || *cfg.failUnder != 15 {
		t.Fatalf("got %+v", cfg)
	}
	if len(cfg.agents) != 2 || cfg.agents[0] != "claude" || cfg.agents[1] != "codex" {
		t.Fatalf("agents %v", cfg.agents)
	}
	cfg, err = parseArgs([]string{"--agent=grok", "--fail-under=8"})
	if err != nil || len(cfg.agents) != 1 || cfg.agents[0] != "grok" || cfg.failUnder == nil || *cfg.failUnder != 8 {
		t.Fatalf("eq form %+v %v", cfg, err)
	}
	if _, err := parseArgs([]string{"--agent", "nope"}); err == nil || !strings.Contains(err.Error(), "unknown agent") {
		t.Fatalf("bad agent err=%v", err)
	}
	if _, err := parseArgs([]string{"--wat"}); err == nil {
		t.Fatal("unknown flag")
	}
}
