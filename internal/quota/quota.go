package quota

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Report struct {
	FetchedAt time.Time `json:"fetched_at"`
	Claude    Claude    `json:"claude"`
	Grok      Grok      `json:"grok"`
	Codex     Codex     `json:"codex"`
}

type Claude struct {
	OK      bool     `json:"ok"`
	Error   string   `json:"error,omitempty"`
	Used5h  *float64 `json:"used_5h,omitempty"`
	Used7d  *float64 `json:"used_7d,omitempty"`
	Reset5h int64    `json:"reset_5h,omitempty"`
	Reset7d int64    `json:"reset_7d,omitempty"`
	Updated int64    `json:"updated_at,omitempty"`
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
	OK      bool    `json:"ok"`
	Error   string  `json:"error,omitempty"`
	Plan    string  `json:"plan,omitempty"`
	Primary *Window `json:"primary,omitempty"`
	Extra   []Extra `json:"extra,omitempty"`
}

type Extra struct {
	Name    string  `json:"name"`
	Primary *Window `json:"primary,omitempty"`
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
	rep := Report{FetchedAt: time.Now(), Claude: readClaude(opt.Home)}
	if cached, ok := readCache(opt.Home, opt.TTL); ok {
		rep.Grok = cached.Grok
		rep.Codex = cached.Codex
		rep.FetchedAt = cached.FetchedAt
		return rep
	}
	rep.Grok = fetchGrok(opt)
	rep.Codex = fetchCodex(opt)
	writeCache(opt.Home, rep)
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
	var raw struct {
		Updated int64 `json:"updated_at"`
		Five    *struct {
			Used  *float64 `json:"used_percentage"`
			Reset int64    `json:"resets_at"`
		} `json:"five_hour"`
		Seven *struct {
			Used  *float64 `json:"used_percentage"`
			Reset int64    `json:"resets_at"`
		} `json:"seven_day"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return Claude{Error: "rate-limits.json unreadable"}
	}
	c := Claude{OK: true, Updated: raw.Updated}
	if raw.Five != nil {
		c.Used5h = raw.Five.Used
		c.Reset5h = raw.Five.Reset
	}
	if raw.Seven != nil {
		c.Used7d = raw.Seven.Used
		c.Reset7d = raw.Seven.Reset
	}
	return c
}

func fetchGrok(opt Options) Grok {
	tok := grokToken(opt.Home)
	if tok == "" {
		return Grok{Error: "no ~/.grok/auth.json token"}
	}
	st, body, err := get(opt.Client, "https://cli-chat-proxy.grok.com/v1/billing?format=credits", tok, "")
	if err != nil {
		return Grok{Error: "grok billing: " + err.Error()}
	}
	if st == 401 {
		return Grok{Error: "grok token expired — run grok login"}
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
		r := 100 - u
		if r < 0 {
			r = 0
		}
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
		return Codex{Error: "codex token expired — run codex login"}
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
				Primary *rawWin `json:"primary_window"`
			} `json:"rate_limit"`
		} `json:"additional_rate_limits"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return Codex{Error: "codex usage parse"}
	}
	c := Codex{OK: true, Plan: raw.Plan}
	if raw.RateLimit != nil {
		c.Primary = decodeWin(raw.RateLimit.Primary)
	}
	for _, a := range raw.Additional {
		ex := Extra{Name: a.Name}
		if a.RateLimit != nil {
			ex.Primary = decodeWin(a.RateLimit.Primary)
		}
		c.Extra = append(c.Extra, ex)
	}
	return c
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
		r := 100 - u
		if r < 0 {
			r = 0
		}
		out.Used = &u
		out.Remaining = &r
	}
	return out
}

func grokToken(home string) string {
	b, err := os.ReadFile(filepath.Join(home, ".grok", "auth.json"))
	if err != nil {
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	for _, v := range m {
		var e struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(v, &e) == nil && e.Key != "" {
			return e.Key
		}
	}
	return ""
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
	b, err := os.ReadFile(cachePath(home))
	if err != nil {
		return Report{}, false
	}
	var r Report
	if json.Unmarshal(b, &r) != nil {
		return Report{}, false
	}
	if time.Since(r.FetchedAt) > ttl {
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
