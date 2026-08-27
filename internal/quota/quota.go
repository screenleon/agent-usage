package quota

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/screenleon/agent-usage/internal/filter"
)

type Report struct {
	FetchedAt time.Time `json:"fetched_at"`
	Claude    Claude    `json:"claude"`
	Grok      Grok      `json:"grok"`
	Codex     Codex     `json:"codex"`
}

type Claude struct {
	OK      bool        `json:"ok"`
	Error   string      `json:"error,omitempty"`
	Used5h  *float64    `json:"used_5h,omitempty"`
	Used7d  *float64    `json:"used_7d,omitempty"`
	Reset5h int64       `json:"reset_5h,omitempty"`
	Reset7d int64       `json:"reset_7d,omitempty"`
	Updated int64       `json:"updated_at,omitempty"`
	Extra   []ClaudeWin `json:"extra,omitempty"`
}

type ClaudeWin struct {
	Name  string   `json:"name"`
	Used  *float64 `json:"used,omitempty"`
	Reset int64    `json:"reset,omitempty"`
}

type Grok struct {
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
	Used      *float64  `json:"used,omitempty"`
	Remaining *float64  `json:"remaining,omitempty"`
	End       string    `json:"end,omitempty"`
	Products  []Product `json:"products,omitempty"`
}

type Product struct {
	Name string  `json:"product"`
	Used float64 `json:"usagePercent"`
}

type Codex struct {
	OK          bool    `json:"ok"`
	Error       string  `json:"error,omitempty"`
	Plan        string  `json:"plan,omitempty"`
	Primary     *Window `json:"primary,omitempty"`
	Secondary   *Window `json:"secondary,omitempty"`
	Extra       []Extra `json:"extra,omitempty"`
	Resets      int     `json:"resets,omitempty"`
	ResetExpiry int64   `json:"reset_expiry,omitempty"`
}

type Extra struct {
	Name      string  `json:"name"`
	Primary   *Window `json:"primary,omitempty"`
	Secondary *Window `json:"secondary,omitempty"`
}

type Window struct {
	Used          *float64 `json:"used,omitempty"`
	Remaining     *float64 `json:"remaining,omitempty"`
	WindowSeconds int64    `json:"window_seconds,omitempty"`
	ResetAfter    int64    `json:"reset_after,omitempty"`
	ResetAt       int64    `json:"reset_at,omitempty"`
}

type Options struct {
	Home   string
	TTL    time.Duration
	Client *http.Client
	Agents []string
}

func Load(opt Options) Report {
	if opt.Home == "" {
		opt.Home = os.Getenv("HOME")
	}
	if opt.TTL == 0 {
		opt.TTL = time.Minute
	}
	if opt.Client == nil {
		opt.Client = &http.Client{Timeout: 8 * time.Second}
	}
	rep := Report{FetchedAt: time.Now()}
	if filter.Wants(opt.Agents, "claude") {
		rep.Claude = readClaude(opt.Home)
	}
	needGrok := filter.Wants(opt.Agents, "grok")
	needCodex := filter.Wants(opt.Agents, "codex")
	if !needGrok && !needCodex {
		return rep
	}
	if cached, ok := readCache(opt.Home, opt.TTL); ok {
		if needGrok {
			rep.Grok = cached.Grok
		}
		if needCodex {
			rep.Codex = cached.Codex
		}
		rep.FetchedAt = cached.FetchedAt
		return rep
	}
	var (
		g  Grok
		c  Codex
		wg sync.WaitGroup
	)
	if needGrok {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g = fetchGrok(opt)
		}()
	}
	if needCodex {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c = fetchCodex(opt)
		}()
	}
	wg.Wait()
	if needGrok {
		rep.Grok = g
	}
	if needCodex {
		rep.Codex = c
	}
	writeCacheMerge(opt.Home, rep, needGrok, needCodex)
	return rep
}

func readClaude(home string) Claude {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(home, ".claude")
	}
	b, err := os.ReadFile(filepath.Join(dir, "rate-limits.json"))
	if err != nil {
		return Claude{Error: "no rate-limits.json"}
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return Claude{Error: "rate-limits.json unreadable"}
	}
	c := Claude{OK: true}
	if v, ok := raw["updated_at"]; ok {
		_ = json.Unmarshal(v, &c.Updated)
	}
	if w := claudeWindowFrom(raw["five_hour"]); w.Used != nil {
		c.Used5h = w.Used
		c.Reset5h = w.Reset
	}
	if w := claudeWindowFrom(raw["seven_day"]); w.Used != nil {
		c.Used7d = w.Used
		c.Reset7d = w.Reset
	}
	for k, v := range raw {
		if k == "updated_at" || k == "five_hour" || k == "seven_day" {
			continue
		}
		w := claudeWindowFrom(v)
		if w.Used == nil {
			continue
		}
		w.Name = claudeWinLabel(k)
		c.Extra = append(c.Extra, w)
	}
	sort.Slice(c.Extra, func(i, j int) bool { return c.Extra[i].Name < c.Extra[j].Name })
	return c
}

func claudeWindowFrom(raw json.RawMessage) ClaudeWin {
	if len(raw) == 0 {
		return ClaudeWin{}
	}
	var o struct {
		Used  *float64 `json:"used_percentage"`
		Reset int64    `json:"resets_at"`
	}
	if json.Unmarshal(raw, &o) != nil {
		return ClaudeWin{}
	}
	return ClaudeWin{Used: o.Used, Reset: o.Reset}
}

func claudeWinLabel(k string) string {
	k = strings.ReplaceAll(k, "seven_day", "7d")
	k = strings.ReplaceAll(k, "five_hour", "5h")
	return strings.ReplaceAll(k, "_", " ")
}

func fetchGrok(opt Options) Grok {
	tok, stale := grokAuth(opt.Home)
	if tok == "" {
		return Grok{Error: "no ~/.grok/auth.json token"}
	}
	if stale {
		return Grok{Error: staleToken("grok")}
	}
	st, body, err := get(opt.Client, "https://cli-chat-proxy.grok.com/v1/billing?format=credits", tok, "")
	if err != nil {
		return Grok{Error: "grok billing: " + err.Error()}
	}
	if st == 401 {
		return Grok{Error: staleToken("grok")}
	}
	if st != 200 {
		return Grok{Error: "grok billing HTTP " + strconv.Itoa(st)}
	}
	var raw struct {
		Config *struct {
			CreditUsagePercent *float64 `json:"creditUsagePercent"`
			CurrentPeriod      *struct {
				End string `json:"end"`
			} `json:"currentPeriod"`
			BillingPeriodEnd string `json:"billingPeriodEnd"`
			ProductUsage     []struct {
				Product      string  `json:"product"`
				UsagePercent float64 `json:"usagePercent"`
			} `json:"productUsage"`
		} `json:"config"`
	}
	if json.Unmarshal(body, &raw) != nil || raw.Config == nil {
		return Grok{Error: "grok billing parse"}
	}
	g := Grok{OK: true}
	if raw.Config.CreditUsagePercent != nil {
		u := *raw.Config.CreditUsagePercent
		r := Remaining(u)
		g.Used = &u
		g.Remaining = &r
	}
	if raw.Config.CurrentPeriod != nil {
		g.End = raw.Config.CurrentPeriod.End
	} else {
		g.End = raw.Config.BillingPeriodEnd
	}
	for _, p := range raw.Config.ProductUsage {
		g.Products = append(g.Products, Product{Name: p.Product, Used: p.UsagePercent})
	}
	return g
}

func fetchCodex(opt Options) Codex {
	tok, acct := codexToken(opt.Home)
	if tok == "" {
		return Codex{Error: "no ~/.codex/auth.json token"}
	}
	st, body, err := get(opt.Client, "https://chatgpt.com/backend-api/wham/usage", tok, acct)
	if err != nil {
		return Codex{Error: "codex usage: " + err.Error()}
	}
	if st == 401 {
		return Codex{Error: staleToken("codex")}
	}
	if st != 200 {
		return Codex{Error: "codex usage HTTP " + strconv.Itoa(st)}
	}
	var raw struct {
		Plan      string `json:"plan_type"`
		RateLimit *struct {
			Primary   *rawWin `json:"primary_window"`
			Secondary *rawWin `json:"secondary_window"`
		} `json:"rate_limit"`
		Additional []struct {
			Name      string `json:"limit_name"`
			RateLimit *struct {
				Primary   *rawWin `json:"primary_window"`
				Secondary *rawWin `json:"secondary_window"`
			} `json:"rate_limit"`
		} `json:"additional_rate_limits"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return Codex{Error: "codex usage parse"}
	}
	c := Codex{OK: true, Plan: raw.Plan}
	if raw.RateLimit != nil {
		c.Primary = decodeWin(raw.RateLimit.Primary)
		c.Secondary = decodeWin(raw.RateLimit.Secondary)
	}
	for _, a := range raw.Additional {
		ex := Extra{Name: a.Name}
		if a.RateLimit != nil {
			ex.Primary = decodeWin(a.RateLimit.Primary)
			ex.Secondary = decodeWin(a.RateLimit.Secondary)
		}
		c.Extra = append(c.Extra, ex)
	}
	fillCodexResets(&c, opt, tok, acct)
	return c
}

func fillCodexResets(c *Codex, opt Options, tok, acct string) {
	st, body, err := get(opt.Client, "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits", tok, acct)
	if err != nil || st != 200 {
		return
	}
	var raw struct {
		Available int `json:"available_count"`
		Credits   []struct {
			Status    string `json:"status"`
			ExpiresAt string `json:"expires_at"`
		} `json:"credits"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return
	}
	c.Resets = raw.Available
	var soon int64
	for _, cr := range raw.Credits {
		if cr.Status != "active" && cr.Status != "available" {
			continue
		}
		t, ok := ParseTime(cr.ExpiresAt)
		if !ok {
			continue
		}
		u := t.Unix()
		if soon == 0 || u < soon {
			soon = u
		}
	}
	c.ResetExpiry = soon
}

type rawWin struct {
	UsedPercent   *float64 `json:"used_percent"`
	WindowSeconds int64    `json:"limit_window_seconds"`
	ResetAfter    int64    `json:"reset_after_seconds"`
	ResetAt       int64    `json:"reset_at"`
}

func decodeWin(w *rawWin) *Window {
	if w == nil {
		return nil
	}
	out := &Window{WindowSeconds: w.WindowSeconds, ResetAfter: w.ResetAfter, ResetAt: w.ResetAt}
	if w.UsedPercent != nil {
		u := *w.UsedPercent
		r := Remaining(u)
		out.Used = &u
		out.Remaining = &r
	}
	return out
}

// Remaining is 100-used, floored at 0.
func Remaining(used float64) float64 {
	r := 100 - used
	if r < 0 {
		return 0
	}
	return r
}

func grokToken(home string) string {
	tok, _ := grokAuth(home)
	return tok
}

func grokAuth(home string) (token string, stale bool) {
	b, err := os.ReadFile(filepath.Join(home, ".grok", "auth.json"))
	if err != nil {
		return "", false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return "", false
	}
	for _, v := range m {
		var e struct {
			Key       string          `json:"key"`
			ExpiresAt json.RawMessage `json:"expires_at"`
		}
		if json.Unmarshal(v, &e) != nil || e.Key == "" {
			continue
		}
		return e.Key, expired(e.ExpiresAt)
	}
	return "", false
}

func expired(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		if t, ok := ParseTime(s); ok {
			return time.Now().After(t)
		}
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		sec := int64(n)
		if n > 1e12 {
			sec = int64(n / 1000)
		}
		return time.Now().After(time.Unix(sec, 0))
	}
	return false
}

func ParseTime(s string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func staleToken(bin string) string {
	return "token stale — start " + bin + " to refresh (not login)"
}

func PlanName(plan string) string {
	switch strings.ToLower(plan) {
	case "prolite":
		return "Pro 5x"
	case "pro":
		return "Pro"
	case "plus":
		return "Plus"
	case "go":
		return "Go"
	default:
		if plan == "" {
			return "?"
		}
		return plan
	}
}

func LimitName(name string) string {
	switch strings.ToLower(name) {
	case "gpt-reserve":
		return "Luna Reserve"
	default:
		return name
	}
}

const LowWater = 15.0

func (r Report) ForAgents(agents []string) Report {
	if len(agents) == 0 {
		return r
	}
	out := r
	if !filter.Wants(agents, "claude") {
		out.Claude = Claude{}
	}
	if !filter.Wants(agents, "grok") {
		out.Grok = Grok{}
	}
	if !filter.Wants(agents, "codex") {
		out.Codex = Codex{}
	}
	return out
}

func (c Codex) windows() []*Window {
	out := []*Window{c.Primary, c.Secondary}
	for _, ex := range c.Extra {
		out = append(out, ex.Primary, ex.Secondary)
	}
	return out
}

func (r Report) MinRemaining(agents []string) (float64, bool) {
	r = r.ForAgents(agents)
	min := 101.0
	found := false
	r.eachRemaining(func(v float64) {
		if !found || v < min {
			min = v
			found = true
		}
	})
	if !found {
		return 0, false
	}
	return min, true
}

func (r Report) eachRemaining(fn func(float64)) {
	if r.Claude.OK {
		for _, u := range []*float64{r.Claude.Used5h, r.Claude.Used7d} {
			if u != nil {
				fn(Remaining(*u))
			}
		}
		for _, w := range r.Claude.Extra {
			if w.Used != nil {
				fn(Remaining(*w.Used))
			}
		}
	}
	if r.Grok.OK {
		if r.Grok.Remaining != nil {
			fn(*r.Grok.Remaining)
		}
		for _, p := range r.Grok.Products {
			fn(Remaining(p.Used))
		}
	}
	if r.Codex.OK {
		for _, w := range r.Codex.windows() {
			if w != nil && w.Remaining != nil {
				fn(*w.Remaining)
			}
		}
	}
}

func codexToken(home string) (token, account string) {
	b, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		return "", ""
	}
	var raw struct {
		Tokens *struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(b, &raw) != nil || raw.Tokens == nil {
		return "", ""
	}
	return raw.Tokens.AccessToken, raw.Tokens.AccountID
}

func get(c *http.Client, url, bearer, account string) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agent-usage/0.1")
	if account != "" {
		req.Header.Set("ChatGPT-Account-Id", account)
	}
	res, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, body, nil
}

func cachePath(home string) string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "agent-usage", "quota.json")
}

func readCache(home string, ttl time.Duration) (Report, bool) {
	r, ok := readCacheFile(home)
	if !ok {
		return Report{}, false
	}
	if time.Since(r.FetchedAt) > ttl {
		return Report{}, false
	}
	return r, true
}

func readCacheFile(home string) (Report, bool) {
	b, err := os.ReadFile(cachePath(home))
	if err != nil {
		return Report{}, false
	}
	var r Report
	if json.Unmarshal(b, &r) != nil {
		return Report{}, false
	}
	return r, true
}

func writeCache(home string, r Report) {
	p := cachePath(home)
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, b, 0o600)
}

func writeCacheMerge(home string, r Report, grok, codex bool) {
	prev, ok := readCacheFile(home)
	if !ok {
		writeCache(home, r)
		return
	}
	if grok {
		prev.Grok = r.Grok
	}
	if codex {
		prev.Codex = r.Codex
	}
	prev.FetchedAt = r.FetchedAt
	writeCache(home, prev)
}
