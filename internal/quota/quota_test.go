package quota

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestGrokConfigured(t *testing.T) {
	home := t.TempDir()
	if grokConfigured(home) {
		t.Fatal("missing auth should not configure Grok")
	}
	dir := filepath.Join(home, ".grok")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !grokConfigured(home) {
		t.Fatal("auth.json should configure Grok")
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func writeQuotaAuth(t *testing.T, home string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, ".grok"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".grok", "auth.json"),
		[]byte(`{"https://auth.x.ai::x":{"key":"g-tok"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"),
		[]byte(`{"tokens":{"access_token":"c-tok","account_id":"acc"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSelectsProvidersAndPreservesCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	writeQuotaAuth(t, home)

	grokBody := `{"config":{"creditUsagePercent":10,"billingPeriodEnd":"2026-08-28T00:00:00Z","productUsage":[]}}`
	codexBody := `{"plan_type":"prolite","rate_limit":{"primary_window":{"used_percent":5,"limit_window_seconds":604800,"reset_after_seconds":100,"reset_at":1}}}`
	var calls []string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Host+r.URL.Path)
		body := grokBody
		if strings.Contains(r.URL.Host, "chatgpt.com") {
			if strings.Contains(r.URL.Path, "reset-credits") {
				return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			}
			body = codexBody
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	opt := func(agents []string) Options {
		return Options{Home: home, Client: client, Agents: agents, TTL: time.Minute}
	}

	r := Load(opt([]string{"grok"}))
	if !r.Grok.OK || r.Codex.OK || r.Grok.Used == nil || *r.Grok.Used != 10 {
		t.Fatalf("grok-only %#v", r)
	}
	if len(calls) != 1 || !strings.Contains(calls[0], "cli-chat-proxy.grok.com") {
		t.Fatalf("grok calls %v", calls)
	}

	calls = nil
	r = Load(opt([]string{"grok"}))
	if len(calls) != 0 || !r.Grok.OK {
		t.Fatalf("fresh cache refetch %v %#v", calls, r)
	}

	calls = nil
	r = Load(opt([]string{"codex"}))
	if !r.Codex.OK || r.Grok.OK || r.Codex.Plan != "prolite" {
		t.Fatalf("codex-only %#v", r)
	}
	if len(calls) != 2 { // usage + reset-credits
		t.Fatalf("codex calls %v", calls)
	}
	cached, ok := readCacheFile(home)
	if !ok || !cached.Grok.OK || !cached.Codex.OK || cached.Grok.Used == nil || *cached.Grok.Used != 10 {
		t.Fatalf("preserved cache %#v", cached)
	}
}

func TestLoadUnconfiguredGrokDefaultVsExplicit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected HTTP call to %s", r.URL)
		return nil, nil
	})}

	r := Load(Options{Home: home, Client: client, TTL: time.Minute})
	if r.Grok.OK || r.Grok.Error != "" {
		t.Fatalf("default view should silently skip unconfigured grok: %#v", r.Grok)
	}

	r = Load(Options{Home: home, Client: client, Agents: []string{"grok"}, TTL: time.Minute})
	if r.Grok.OK || r.Grok.Error == "" {
		t.Fatalf("explicit --agent grok should still report the missing-token error: %#v", r.Grok)
	}
}

func TestLoadConcurrent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache-conc"))
	writeQuotaAuth(t, home)

	grokBody := `{"config":{"creditUsagePercent":42,"billingPeriodEnd":"2026-08-28T00:00:00Z","productUsage":[]}}`
	codexBody := `{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":7,"limit_window_seconds":604800,"reset_after_seconds":100,"reset_at":1}}}`
	grokStarted := make(chan struct{})
	codexStarted := make(chan struct{})
	release := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Host, "chatgpt.com") {
			if strings.Contains(r.URL.Path, "reset-credits") {
				<-release
				return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			}
			codexStarted <- struct{}{}
			<-release
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(codexBody)), Header: make(http.Header)}, nil
		}
		grokStarted <- struct{}{}
		<-release
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(grokBody)), Header: make(http.Header)}, nil
	})}

	done := make(chan Report, 1)
	go func() {
		done <- Load(Options{Home: home, Client: client, TTL: time.Minute})
	}()

	timeout := time.After(3 * time.Second)
	seenGrok, seenCodex := false, false
	for !seenGrok || !seenCodex {
		select {
		case <-grokStarted:
			seenGrok = true
		case <-codexStarted:
			seenCodex = true
		case <-timeout:
			t.Fatalf("both providers did not start; grok=%v codex=%v", seenGrok, seenCodex)
		}
	}
	close(release)

	select {
	case r := <-done:
		if !r.Grok.OK || r.Grok.Used == nil || *r.Grok.Used != 42 {
			t.Fatalf("grok %#v", r.Grok)
		}
		if !r.Codex.OK || r.Codex.Plan != "plus" || r.Codex.Primary == nil || r.Codex.Primary.Used == nil || *r.Codex.Primary.Used != 7 {
			t.Fatalf("codex %#v", r.Codex)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Load did not finish after release")
	}
}

func TestFillCodexResetsSelectsEarliestActive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache-resets"))
	writeQuotaAuth(t, home)
	usageBody := `{"plan_type":"prolite","rate_limit":{"primary_window":{"used_percent":1,"limit_window_seconds":604800,"reset_after_seconds":100,"reset_at":1}}}`
	resetBody := `{"available_count":2,"credits":[{"status":"expired","expires_at":"2026-01-01T00:00:00Z"},{"status":"active","expires_at":"2026-09-10T00:00:00Z"},{"status":"available","expires_at":"2026-09-01T00:00:00Z"},{"status":"used","expires_at":"2026-08-01T00:00:00Z"}]}`
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := usageBody
		if strings.Contains(r.URL.Path, "reset-credits") {
			body = resetBody
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	r := Load(Options{Home: home, Client: client, Agents: []string{"codex"}, TTL: time.Minute})
	if !r.Codex.OK || r.Codex.Resets != 2 {
		t.Fatalf("resets %#v", r.Codex)
	}
	want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).Unix()
	if r.Codex.ResetExpiry != want {
		t.Fatalf("expiry %d want %d", r.Codex.ResetExpiry, want)
	}
}

func TestClaudeWinLabelStripsControls(t *testing.T) {
	got := claudeWinLabel("seven_day_\x1b]52;c;evil\x07_win\n")
	if strings.ContainsRune(got, 0x1b) || strings.ContainsRune(got, 0x07) || strings.ContainsRune(got, '\n') {
		t.Fatalf("controls remain %q", got)
	}
	if !strings.Contains(got, "7d") {
		t.Fatalf("label %q", got)
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
