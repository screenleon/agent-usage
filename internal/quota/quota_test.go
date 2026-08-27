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
	if _, stale := grokAuth(home); stale {
		t.Fatal("missing expires_at should not be stale")
	}
}

// grokAuth reports stale when expires_at is in the past.
func TestGrokAuthExpired(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".grok")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"https://auth.x.ai::abc":{"key":"tok-1","expires_at":"2000-01-01T00:00:00Z"}}`
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, stale := grokAuth(home)
	if tok != "tok-1" || !stale {
		t.Fatalf("tok=%q stale=%v", tok, stale)
	}
}

// readClaude keeps extra windows besides five_hour and seven_day.
func TestReadClaudeExtraWindows(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"updated_at":100,"five_hour":{"used_percentage":17,"resets_at":200},"seven_day":{"used_percentage":14,"resets_at":300},"seven_day_sonnet":{"used_percentage":40,"resets_at":400}}`
	if err := os.WriteFile(filepath.Join(dir, "rate-limits.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c := readClaude(home)
	if !c.OK || c.Used5h == nil || *c.Used5h != 17 || len(c.Extra) != 1 || c.Extra[0].Name != "7d sonnet" || c.Extra[0].Used == nil || *c.Extra[0].Used != 40 {
		t.Fatalf("got %#v", c)
	}
}

func TestParseTime(t *testing.T) {
	t1, ok := ParseTime("2026-08-28T17:11:41.327003+00:00")
	if !ok || t1.Year() != 2026 {
		t.Fatalf("nano %v %v", t1, ok)
	}
	t2, ok := ParseTime("2026-08-28T17:11:41Z")
	if !ok || t2.Year() != 2026 {
		t.Fatalf("rfc3339 %v %v", t2, ok)
	}
	if _, ok := ParseTime("not-a-time"); ok {
		t.Fatal("bad")
	}
}

func TestPlanAndLimitNames(t *testing.T) {
	if PlanName("prolite") != "Pro 5x" || PlanName("pro") != "Pro" {
		t.Fatal(PlanName("prolite"))
	}
	if LimitName("gpt-reserve") != "Luna Reserve" {
		t.Fatal(LimitName("gpt-reserve"))
	}
}

func TestMinRemaining(t *testing.T) {
	u := 90.0
	r := Remaining(u)
	rep := Report{
		Claude: Claude{OK: true, Used5h: &u},
		Grok:   Grok{OK: true, Remaining: &r},
	}
	min, ok := rep.MinRemaining(nil)
	if !ok || min != 10 {
		t.Fatalf("min=%v ok=%v", min, ok)
	}
	min, ok = rep.MinRemaining([]string{"grok"})
	if !ok || min != 10 {
		t.Fatalf("grok min=%v", min)
	}
	if _, ok := rep.MinRemaining([]string{"codex"}); ok {
		t.Fatal("codex has no windows")
	}
}

func TestForAgents(t *testing.T) {
	u := 1.0
	rep := Report{Claude: Claude{OK: true, Used5h: &u}, Grok: Grok{OK: true}}
	got := rep.ForAgents([]string{"claude"})
	if !got.Claude.OK || got.Grok.OK {
		t.Fatalf("got %#v", got)
	}
}
