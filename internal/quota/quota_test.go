package quota

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestRemaining(t *testing.T) {
	if Remaining(33) != 67 || Remaining(120) != 0 {
		t.Fatalf("Remaining(33)=%v Remaining(120)=%v", Remaining(33), Remaining(120))
	}
}

func TestDecodeWin(t *testing.T) {
	u := 33.0
	w := decodeWin(&rawWin{UsedPercent: &u, WindowSeconds: 604800, ResetAfter: 10})
	if w == nil || w.Remaining == nil || *w.Remaining != 67 {
		t.Fatalf("got %#v", w)
	}
}

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
