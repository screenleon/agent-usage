package quota

import (
	"os"
	"path/filepath"
	"testing"
)

// readClaude loads five-hour and seven-day used percentages from rate-limits.json.
// Steps:
// 1. Write a rate-limits.json under a temp home.
// 2. Call readClaude.
// 3. Expect used 17 and reset 200 on the five-hour window.
func TestReadClaude(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"updated_at":100,"five_hour":{"used_percentage":17,"resets_at":200},"seven_day":{"used_percentage":14,"resets_at":300}}`
	if err := os.WriteFile(filepath.Join(dir, "rate-limits.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c := readClaude(home)
	if !c.OK || c.Used5h == nil || *c.Used5h != 17 || c.Reset5h != 200 {
		t.Fatalf("got %#v", c)
	}
}

// Remaining is 100 minus used and never goes below zero.
// Steps:
// 1. Choose an in-range used percent and one above 100.
// 2. Call Remaining.
// 3. Expect 67 and 0.
func TestRemaining(t *testing.T) {
	if Remaining(33) != 67 || Remaining(120) != 0 {
		t.Fatalf("Remaining(33)=%v Remaining(120)=%v", Remaining(33), Remaining(120))
	}
}

// decodeWin fills remaining percent from used_percent.
// Steps:
// 1. Build a raw window with used 33.
// 2. Call decodeWin.
// 3. Expect remaining 67.
func TestDecodeWin(t *testing.T) {
	u := 33.0
	w := decodeWin(&rawWin{UsedPercent: &u, WindowSeconds: 604800, ResetAfter: 10})
	if w == nil || w.Remaining == nil || *w.Remaining != 67 {
		t.Fatalf("got %#v", w)
	}
}

// grokToken reads the first auth.json key field.
// Steps:
// 1. Write a one-entry auth.json.
// 2. Call grokToken.
// 3. Expect tok-1.
func TestGrokToken(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".grok")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"https://auth.x.ai::abc":{"key":"tok-1","auth_mode":"oidc"}}`
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if g := grokToken(home); g != "tok-1" {
		t.Fatalf("got %q", g)
	}
}
