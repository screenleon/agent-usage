package filter

import "testing"

func TestWants(t *testing.T) {
	if !Wants(nil, "claude") || !Wants([]string{}, "grok") {
		t.Fatal("empty means all")
	}
	if Wants([]string{"codex"}, "claude") || !Wants([]string{"codex", "grok"}, "grok") {
		t.Fatal("filter")
	}
}
