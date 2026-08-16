package render

import (
	"strconv"
	"strings"
	"testing"

	"github.com/screenleon/agent-usage/internal/collect"
)

func TestSortSessions(t *testing.T) {
	in := []collect.Session{
		{Status: "idle", Agent: "grok", PID: 2},
		{Status: "busy", Agent: "codex", PID: 9},
		{Status: "busy", Agent: "claude", PID: 3},
		{Status: "run", Agent: "claude", PID: 1},
		{Status: "busy", Agent: "claude", PID: 1},
	}
	origPID := make([]int, len(in))
	for i, s := range in {
		origPID[i] = s.PID
	}
	got := sortSessions(in)
	want := []string{"busy/claude/1", "busy/claude/3", "busy/codex/9", "run/claude/1", "idle/grok/2"}
	if len(got) != len(want) {
		t.Fatalf("len %d", len(got))
	}
	for i, w := range want {
		g := got[i].Status + "/" + got[i].Agent + "/" + strconv.Itoa(got[i].PID)
		if g != w {
			t.Fatalf("row %d = %s want %s", i, g, w)
		}
	}
	for i, s := range in {
		if s.PID != origPID[i] {
			t.Fatalf("input mutated at %d", i)
		}
	}
}

func TestTruncTitle(t *testing.T) {
	if g := TruncTitle("  hello\nworld  ", 40); g != "hello world" {
		t.Fatalf("newline: %q", g)
	}
	exact := strings.Repeat("a", 40)
	if g := TruncTitle(exact, 40); g != exact {
		t.Fatalf("40-byte: %q", g)
	}
	over := strings.Repeat("b", 41)
	if g := TruncTitle(over, 40); g != strings.Repeat("b", 40) {
		t.Fatalf("41-byte: %q", g)
	}
}
