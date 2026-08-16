package collect

import "testing"

// classify maps process comm/argv0 to a known agent name and ignores helpers.
// Steps:
// 1. Build representative comm and cmdline pairs.
// 2. Call classify for each pair.
// 3. Expect the agent name or an empty string for non-agents.
func TestClassify(t *testing.T) {
	cases := []struct {
		comm, cmd, want string
	}{
		{"claude", "claude", "claude"},
		{"grok", "/home/u/.grok/bin/grok", "grok"},
		{"grok-1.0.4", "/home/u/.grok/bin/grok-1.0.4", "grok"},
		{"codex", "codex", "codex"},
		{"codex", "codex exec --cd /home/u/github/agent-usage --json", "codex"},
		{"opencode", "opencode", "opencode"},
		{"node", "/usr/bin/node /home/u/.nvm/bin/grok", ""},
		{"node", "/usr/bin/node /home/u/.local/bin/claude", "claude"},
		{"bash", "agent-usage", ""},
		{"codex-code-mode", "/home/u/.codex/bin/codex-code-mode-host", ""},
	}
	for _, c := range cases {
		if g := classify(c.comm, c.cmd); g != c.want {
			t.Fatalf("classify(%q,%q)=%q want %q", c.comm, c.cmd, g, c.want)
		}
	}
}

// isSelfMonitor matches only the monitor executable, not other argv text.
// Steps:
// 1. Prepare monitor and Codex command lines, including ones that mention this repo.
// 2. Call isSelfMonitor.
// 3. Expect true only when comm or argv0 is agent-usage.
func TestIsSelfMonitor(t *testing.T) {
	cases := []struct {
		comm, cmd string
		want      bool
	}{
		{"agent-usage", "/home/u/.local/bin/agent-usage watch", true},
		{"agent-usage", "agent-usage", true},
		{"codex", "codex exec --cd /home/u/github/agent-usage --json", false},
		{"codex", "codex", false},
		{"bash", "bash /tmp/x.sh --repo agent-usage", false},
	}
	for _, c := range cases {
		if g := isSelfMonitor(c.comm, c.cmd); g != c.want {
			t.Fatalf("isSelfMonitor(%q,%q)=%v want %v", c.comm, c.cmd, g, c.want)
		}
	}
}

// flagValue keeps --cd paths that contain spaces in both argv forms.
// Steps:
// 1. Build NUL-delimited Codex argv using separated and equals-form --cd values.
// 2. Call flagValue for --cd.
// 3. Expect the complete directory including spaces.
func TestFlagValue(t *testing.T) {
	sep := "codex\x00exec\x00--cd\x00/work/My Project\x00--json"
	if g := flagValue(sep, "--cd"); g != "/work/My Project" {
		t.Fatalf("separated --cd: %q", g)
	}
	eq := "codex\x00exec\x00--cd=/work/My Project\x00--json"
	if g := flagValue(eq, "--cd"); g != "/work/My Project" {
		t.Fatalf("equals --cd: %q", g)
	}
	plain := "codex exec --cd /home/u/github/agent-usage --json"
	if g := flagValue(plain, "--cd"); g != "/home/u/github/agent-usage" {
		t.Fatalf("plain: %q", g)
	}
}

// parseChildPIDs keeps only decimal process IDs from a children file.
// Steps:
// 1. Feed empty, whitespace, malformed, and multi-id strings.
// 2. Call parseChildPIDs.
// 3. Expect only parseable IDs in order.
func TestChildPIDs(t *testing.T) {
	cases := []struct {
		in   string
		want []int
	}{
		{"", nil},
		{"   \n", nil},
		{"12 34 56", []int{12, 34, 56}},
		{"12 x 34", []int{12, 34}},
		{"not-a-pid", nil},
		{"  7\t8\n9 ", []int{7, 8, 9}},
	}
	for _, c := range cases {
		got := parseChildPIDs(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("parseChildPIDs(%q)=%v want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("parseChildPIDs(%q)=%v want %v", c.in, got, c.want)
			}
		}
	}
}

// formatElapsed renders process age as mm:ss or h:mm:ss.
// Steps:
// 1. Choose elapsed seconds just over one minute and one hour.
// 2. Call formatElapsed.
// 3. Expect zero-padded clock strings.
func TestFormatElapsed(t *testing.T) {
	if g := formatElapsed(65); g != "01:05" {
		t.Fatalf("got %s", g)
	}
	if g := formatElapsed(3661); g != "1:01:01" {
		t.Fatalf("got %s", g)
	}
}

// SanitizeDisplay removes ESC and other control bytes from display text.
// Steps:
// 1. Build a path containing an OSC clipboard sequence.
// 2. Call SanitizeDisplay.
// 3. Expect no ESC or C0/C1 control bytes to remain.
func TestSanitizeDisplay(t *testing.T) {
	got := SanitizeDisplay("/tmp/\x1b]52;c;evil\x07proj")
	if containsESC(got) {
		t.Fatalf("ESC remains: %q", got)
	}
	for _, r := range got {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("control rune %U remains in %q", r, got)
		}
	}
}

func containsESC(s string) bool {
	for _, r := range s {
		if r == 0x1b {
			return true
		}
	}
	return false
}
