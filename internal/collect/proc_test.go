package collect

import "testing"

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

func TestFlagValue(t *testing.T) {
	cmd := `codex exec --cd /home/u/github/agent-usage --json`
	if g := flagValue(cmd, "--cd"); g != "/home/u/github/agent-usage" {
		t.Fatalf("got %q", g)
	}
}

func TestFormatElapsed(t *testing.T) {
	if g := formatElapsed(65); g != "01:05" {
		t.Fatalf("got %s", g)
	}
	if g := formatElapsed(3661); g != "1:01:01" {
		t.Fatalf("got %s", g)
	}
}
