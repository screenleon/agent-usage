# agent-usage

Lightweight live monitor for coding-agent sessions on Linux.

It is the small replacement for [abtop](https://github.com/graykode/abtop) / `aum`:
one binary, no TUI framework, no transcript scan.

```
agent-usage           # one snapshot
agent-usage watch     # refresh every 2s
agent-usage watch 1
agent-usage --offline
agent-usage --json
```

## What it reads

| Source | Files | Why it stays cheap |
| --- | --- | --- |
| Processes | `/proc/<pid>/{comm,cmdline,cwd,stat,children}` | only pids whose `comm` is claude / grok / codex / opencode |
| Claude sessions | `~/.claude/sessions/<pid>.json` | skip if that pid is not a live `claude` |
| Claude tokens / CTX | last **8KiB** of the matching project jsonl ÷ 200k (or 1M if the model name contains `1m`) | no full history walk |
| Codex tokens / CTX | `state_5.sqlite` `tokens_used` ÷ `models_cache.json` `context_window` (× effective %) | one cache file + one SQL row |
| Grok sessions | `~/.grok/active_sessions.json`, `signals.json`, `summary.json` | never opens `updates.jsonl` |
| Codex recent | `sqlite3 -readonly` on `~/.codex/state_5.sqlite` | only with `--recent` or a live `codex` process |
| Claude quota | `~/.claude/rate-limits.json` | written by the existing StatusLine hook |
| Grok remaining | `GET …/v1/billing?format=credits` | cached 60s under `~/.cache/agent-usage/` |
| Codex remaining | `GET …/wham/usage` | same 60s cache |

Quota HTTP uses the tokens already stored in `~/.grok/auth.json` and `~/.codex/auth.json`. Those files are never printed or copied. A Grok **401** means the stored access token is stale: start `grok` once so the CLI can refresh it. That is not a prompt to run `grok login` unless refresh itself fails.

## Why Go

The cost of `aum` / `abtop` was **what they scanned**, not the language they were written in. Both are Rust TUIs that keep a large session index warm.

For this tool:

| Option | Fit |
| --- | --- |
| **Go (this repo)** | one static Linux binary (`CGO_ENABLED=0`), stdlib HTTP/JSON, long-lived `watch` loop, no interpreter respawn |
| Rust | lowest RSS, but same I/O-bound work; more code for the same `/proc` + JSON reads |
| Python / bash | fine for a one-shot; `watch` that forks Python every 2s is the waste we removed |

If resident size ever matters more than compile speed, a Rust port can keep the same file list and cache rules.

## Install

Linux only (reads `/proc`). No Go toolchain required. Optional: `sqlite3` on `PATH` for Codex tokens/CTX (`-json` preferred; older binaries fall back to USV, flattening newlines and USV bytes in titles so later columns stay aligned).

```bash
arch=$(uname -m)
case "$arch" in
  x86_64) suffix=amd64 ;;
  aarch64|arm64) suffix=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac
base=https://github.com/screenleon/agent-usage/releases/latest/download
dir=$(mktemp -d)
curl -fsSL -o "$dir/agent-usage-linux-${suffix}" "$base/agent-usage-linux-${suffix}"
curl -fsSL -o "$dir/SHA256SUMS" "$base/SHA256SUMS"
(cd "$dir" && sha256sum -c SHA256SUMS --ignore-missing)
mkdir -p ~/.local/bin
install -m 755 "$dir/agent-usage-linux-${suffix}" ~/.local/bin/agent-usage
rm -rf "$dir"
```

`SHA256SUMS` is on the same GitHub Release as the binary and lists both architectures; `--ignore-missing` checks only the file you downloaded. That step detects a truncated or corrupted download. It does not authenticate the publisher: a substituted binary shipped with a matching `SHA256SUMS` from the same URL will still pass. Trust the file only if you trust this repository's releases.

### Build from source

```bash
git clone git@github.com:screenleon/agent-usage.git
cd agent-usage
make install          # -> ~/.local/bin/agent-usage
```

Requires Go 1.21+. `make release` writes `dist/agent-usage-linux-{amd64,arm64}` for copying to machines that only need the binary.

## Not in scope

Themes, mouse, kill-session, orphan ports, terminal jump, subagent trees.
Those belong in a dashboard, not in the hot path of a quota/session glance.
