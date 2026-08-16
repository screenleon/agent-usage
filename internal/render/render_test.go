package render

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/screenleon/agent-usage/internal/collect"
)

// sortSessions orders by status, then agent, then PID without mutating input.
// Steps:
// 1. Build an unsorted session slice spanning busy/run/idle and duplicate keys.
// 2. Call sortSessions and snapshot the original PIDs.
// 3. Expect the documented key order and an unchanged input slice.
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

// Snapshot prints a populated CTX percent and a dash when CTX is empty.
// Steps:
// 1. Build a snapshot with one session that has Ctx 21% and one with no Ctx.
// 2. Call Snapshot into a buffer.
// 3. Expect 21% on the first data row and a CTX placeholder on the second.
func TestSnapshotRendersCtxOrPlaceholder(t *testing.T) {
	tok := 56000.0
	var buf bytes.Buffer
	Snapshot(&buf, collect.Snapshot{
		Taken: time.Unix(0, 0).UTC(),
		Sessions: []collect.Session{
			{Status: "busy", Agent: "codex", PID: 1, Ctx: "21%", Tokens: &tok, Dir: "proj"},
			{Status: "run", Agent: "claude", PID: 2, Dir: "other"},
		},
	}, nil, 0)
	out := buf.String()
	if !strings.Contains(out, "21%") {
		t.Fatalf("missing populated ctx:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	var data []string
	for _, ln := range lines {
		if strings.HasPrefix(ln, "busy") || strings.HasPrefix(ln, "run") {
			data = append(data, ln)
		}
	}
	if len(data) != 2 {
		t.Fatalf("data rows %v", data)
	}
	if !strings.Contains(data[0], "21%") {
		t.Fatalf("first row %q", data[0])
	}
	if strings.Contains(data[1], "%") {
		t.Fatalf("empty ctx should be placeholder: %q", data[1])
	}
	if !strings.Contains(data[1], "     -") && !strings.Contains(data[1], "  -  ") {
		// CTX column is %5s; empty becomes "-"
		fields := strings.Fields(data[1])
		// run claude 2 - - - other  → ctx is one of the dashes
		dash := 0
		for _, f := range fields {
			if f == "-" {
				dash++
			}
		}
		if dash < 1 {
			t.Fatalf("no placeholder in %q", data[1])
		}
	}
}

// TruncTitle flattens newlines, honors the 40-byte limit, and strips ESC.
// Steps:
// 1. Pass a multiline title, 40-byte, 41-byte, and ESC-bearing strings.
// 2. Call TruncTitle with limit 40.
// 3. Expect one line, exact 40 preserved, 41 clipped, and no ESC remaining.
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
	hostile := TruncTitle("/tmp/\x1b]52;c;evil\x07x", 40)
	if strings.ContainsRune(hostile, 0x1b) || strings.ContainsRune(hostile, 0x07) {
		t.Fatalf("control bytes remain: %q", hostile)
	}
}
