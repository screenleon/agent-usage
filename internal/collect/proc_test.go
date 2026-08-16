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
		{"opencode", "opencode", "opencode"},
		{"node", "/usr/bin/node /home/u/.nvm/bin/grok", ""},
		{"node", "/usr/bin/node /home/u/.local/bin/claude", "claude"},
		{"bash", "agent-usage", ""},
	}
	for _, c := range cases {
		if g := classify(c.comm, c.cmd); g != c.want {
			t.Fatalf("classify(%q,%q)=%q want %q", c.comm, c.cmd, g, c.want)
		}
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
