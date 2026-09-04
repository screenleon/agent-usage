package main

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestParseArgsRejectsMalformed(t *testing.T) {
	cases := []struct {
		name string
		args []string
		err  string
	}{
		{"missing agent value", []string{"--agent"}, "--agent requires a value"},
		{"empty agent equals", []string{"--agent="}, "--agent requires a value"},
		{"empty agent segments", []string{"--agent", ",,,"}, "--agent requires a value"},
		{"unknown agent", []string{"--agent", "gemini"}, `unknown agent "gemini"`},
		{"missing fail-under", []string{"--fail-under"}, "--fail-under requires a value"},
		{"empty fail-under equals", []string{"--fail-under="}, "--fail-under requires a value"},
		{"negative fail-under", []string{"--fail-under", "-1"}, `invalid --fail-under "-1"`},
		{"non-numeric fail-under", []string{"--fail-under", "abc"}, `invalid --fail-under "abc"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseArgs(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("args %v err=%v want %q", tc.args, err, tc.err)
			}
		})
	}
}

func TestParseArgsRejectsNonFiniteFailUnder(t *testing.T) {
	for _, v := range []string{"NaN", "+Inf", "-Inf", "inf", "+inf", "-inf"} {
		_, err := parseArgs([]string{"--fail-under", v})
		if err == nil || !strings.Contains(err.Error(), "invalid --fail-under") {
			t.Fatalf("%s: err=%v", v, err)
		}
	}
	cfg, err := parseArgs([]string{"--fail-under", "0"})
	if err != nil || cfg.failUnder == nil || *cfg.failUnder != 0 {
		t.Fatalf("zero threshold %+v %v", cfg, err)
	}
}

func TestParseArgsWatchBoundaries(t *testing.T) {
	cfg, err := parseArgs([]string{"watch", "0", "--offline"})
	if err != nil || !cfg.watch || cfg.interval != time.Second || !cfg.offline {
		t.Fatalf("watch 0 %+v %v", cfg, err)
	}
	cfg, err = parseArgs([]string{"-w", "-3", "--json"})
	if err != nil || !cfg.watch || cfg.interval != time.Second || !cfg.asJSON {
		t.Fatalf("watch -3 %+v %v", cfg, err)
	}
	cfg, err = parseArgs([]string{"--agent", "claude,,claude,grok"})
	if err != nil || len(cfg.agents) != 2 || cfg.agents[0] != "claude" || cfg.agents[1] != "grok" {
		t.Fatalf("dup empty segments %+v %v", cfg, err)
	}
}

func TestWatchFailUnderRestoresCursor(t *testing.T) {
	var buf bytes.Buffer
	n := 10.0
	code := runWatch(&buf, func() int { return 1 }, time.Hour, &n, nil)
	if code != 1 {
		t.Fatalf("code %d", code)
	}
	out := buf.String()
	hide := strings.Index(out, cursorHide)
	show := strings.LastIndex(out, cursorShow)
	if runtime.GOOS == "windows" {
		if hide >= 0 || show >= 0 {
			t.Fatalf("unexpected ANSI controls in plain Windows output: %q", out)
		}
		return
	}
	if hide < 0 || show < 0 || show < hide {
		t.Fatalf("cursor restore missing: %q", out)
	}
}
