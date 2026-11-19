package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/badam1/personal-review-bot/internal/config"
	"github.com/badam1/personal-review-bot/internal/store"
	"github.com/badam1/personal-review-bot/internal/update"
	"github.com/badam1/personal-review-bot/internal/version"
)

// The TUI opens links with the platform's own opener, and knows where it has none.
func TestOpener(t *testing.T) {
	cases := map[string]string{"darwin": "open", "linux": "xdg-open", "freebsd": "xdg-open", "windows": ""}
	for goos, want := range cases {
		got, ok := opener(goos)
		if got != want || ok != (want != "") {
			t.Errorf("opener(%q) = %q, %v; want %q", goos, got, ok, want)
		}
	}
}

// The TUI says when a newer release is out (u in the TUI, the CLI command in `status`), or what an update is doing.
func TestUpdateLine(t *testing.T) {
	newer := update.Release{Version: "v9.9.9"}
	cases := []struct {
		s           snapshot
		interactive bool
		want, not   string
	}{
		{snapshot{}, true, "", ""},
		{snapshot{update: newer}, true, "v9.9.9 is available (running ", ""},
		{snapshot{update: newer}, true, "u to update", "personal-review-bot update"},
		{snapshot{update: newer}, false, "personal-review-bot update", "u to update"},
		{snapshot{update: newer, updateState: "installing v9.9.9"}, true, "updating: installing v9.9.9", "is available"},
		{snapshot{update: newer, updateState: "updating to v9.9.9 failed: no attestation"}, true, "update: updating to v9.9.9 failed: no attestation\nv9.9.9 is available", ""},
		{snapshot{daemonVersion: "v9.9.9", updated: &update.Done{From: "v9.9.8", To: "v9.9.9", At: time.Now().Add(-3 * time.Minute)}},
			false, "✓ updated to v9.9.9 (from v9.9.8), 3m0s ago", "restart it"},
		{snapshot{daemonVersion: "v9.9.9"}, true, "the daemon runs v9.9.9, this TUI " + version.String() + ": restart it", ""},
		{snapshot{daemonVersion: version.String()}, true, "", ""},
	}
	for _, c := range cases {
		got := updateLine(c.s, c.interactive)
		if c.want == "" && got != "" || !strings.Contains(got, c.want) || c.not != "" && strings.Contains(got, c.not) {
			t.Errorf("updateLine(%+v, %v) = %q; want %q, not %q", c.s, c.interactive, got, c.want, c.not)
		}
	}
}

// The logo is the icon in half blocks: 16 columns by 8 rows, square in a terminal.
func TestLogo(t *testing.T) {
	lines := strings.Split(logo(), "\n")
	if len(lines) != 8 {
		t.Fatalf("%d rows, want 8", len(lines))
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 16 {
			t.Errorf("row %d is %d columns wide, want 16", i, w)
		}
	}
	for _, row := range strings.Fields(logoSprite) {
		if len(row) != 16 || strings.Trim(row, ".blmg") != "" {
			t.Errorf("sprite row %q: want 16 of . b l m g", row)
		}
	}
}

// The header shows the version, and the logo beside it only in the TUI on a terminal with room for it.
func TestHeader(t *testing.T) {
	cfg := config.Config{Threshold: 80, AutoPost: []string{"low", "medium"}, WebAddr: "127.0.0.1:12346", UpdateRepo: "acme/prb"}
	roomy := header(cfg, snapshot{}, true, 100, 40)
	if !strings.Contains(roomy, "▀") || strings.Contains(roomy, "🤖") || !strings.Contains(roomy, "personal-review-bot "+version.String()) {
		t.Errorf("roomy terminal: want the logo, no emoji, and the version:\n%s", roomy)
	}
	for _, c := range []struct {
		interactive   bool
		width, height int
	}{{true, 50, 40}, {true, 100, 20}, {false, 160, 0}} {
		if h := header(cfg, snapshot{}, c.interactive, c.width, c.height); strings.Contains(h, "▀") || !strings.Contains(h, "🤖 personal-review-bot") {
			t.Errorf("%+v: want no logo and the emoji title:\n%s", c, h)
		}
	}
	if screen := render(cfg, snapshot{}, 0, "", true, 100, 40); !strings.Contains(screen, "bugs and ideas: github.com/acme/prb/issues") {
		t.Error("the TUI should say where to report bugs")
	}
}

// Each review shows what it cost, a running one what it has cost so far, and the header what was spent today and this
// week.
func TestCosts(t *testing.T) {
	for usd, want := range map[float64]string{0: "–", 0.004: "–", 0.02: "$0.02", 5.294: "$5.29", 113.386: "$113.39"} {
		if got := cost(usd); got != want {
			t.Errorf("cost(%v) = %q, want %q", usd, got, want)
		}
	}
	cfg := config.Config{Threshold: 80, AutoPost: []string{"low", "medium"}, WebAddr: "127.0.0.1:12346", UpdateRepo: "acme/prb"}
	s := snapshot{today: store.Totals{Reviews: 4, BotPosted: 3, CostUSD: 12.5}, week: store.Totals{Reviews: 20, BotPosted: 1, CostUSD: 80.25},
		active: []store.Review{{Repo: "acme/api", Number: 3, Title: "Add retries", Status: store.Running, CostUSD: 0.38}},
		recent: []store.Review{{Repo: "acme/api", Number: 1, Title: "Fix login", Author: "dev", Status: store.Done, CostUSD: 1.4},
			{Repo: "acme/api", Number: 2, Title: "Old one", Author: "dev", Status: store.Ignored}}}
	screen := render(cfg, s, 0, "", false, 160, 0)
	for _, want := range []string{"today: 4 reviews · 3 findings posted · $12.50", "7 days: 20 reviews · 1 finding posted · $80.25",
		"$0.38 so far", "  $1.40 ", "      – "} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen is missing %q:\n%s", want, screen)
		}
	}
}

// The gate is the output tray: reviews waiting for you get their own section, riskiest first — the highest-severity
// finding, then the review the verifier was least sure about — each labelled with its confidence, count and cost.
func TestGate(t *testing.T) {
	cfg := config.Config{Threshold: 80, AutoPost: []string{"low"}, WebAddr: "127.0.0.1:12399", UpdateRepo: "acme/prb"}
	awaiting := []store.Review{
		{ID: 1, Repo: "acme/api", Number: 11, Author: "dev", Status: store.AwaitingYou, Approval: 95, Decision: "approve with nits", CostUSD: 0.5},
		{ID: 2, Repo: "acme/api", Number: 22, Author: "dev", Status: store.AwaitingYou, Approval: 30, Decision: "needs changes", CostUSD: 1.2},
		{ID: 3, Repo: "acme/api", Number: 33, Author: "dev", Status: store.AwaitingYou, Approval: 60, Decision: "take a look", CostUSD: 0.8},
	}
	pending := map[int64]store.Waiting{1: {Count: 1, TopSeverity: "low"}, 2: {Count: 3, TopSeverity: "high"}, 3: {}}
	sortAwaiting(awaiting, pending)
	screen := render(cfg, snapshot{awaiting: awaiting, pending: pending}, -1, "", false, 160, 0)
	if !strings.Contains(screen, "WAITING FOR YOU   3") {
		t.Errorf("gate header missing the count:\n%s", screen)
	}
	// Order: #22 (high severity) first, then #11 (low, but very confident sorts above none), then #33 (no findings).
	i22, i11, i33 := strings.Index(screen, "api#22"), strings.Index(screen, "api#11"), strings.Index(screen, "api#33")
	if !(i22 >= 0 && i22 < i11 && i11 < i33) {
		t.Errorf("gate out of risk order: #22=%d #11=%d #33=%d\n%s", i22, i11, i33, screen)
	}
	for _, want := range []string{"3 findings · top high", "no findings", "95%", "30%", "needs changes"} {
		if !strings.Contains(screen, want) {
			t.Errorf("gate is missing %q:\n%s", want, screen)
		}
	}
}

// A review that has been "running" far longer than any real review is almost certainly a dead agent; the view flags it
// so it isn't mistaken for live work, and leaves a fresh one alone.
func TestStuck(t *testing.T) {
	cfg := config.Config{WebAddr: "127.0.0.1:12388", UpdateRepo: "acme/prb"}
	active := []store.Review{
		{Repo: "acme/api", Number: 9, Title: "Big one", Status: store.Running, StartedAt: time.Now().Add(-(staleReview + time.Minute))},
		{Repo: "acme/api", Number: 8, Title: "Quick one", Status: store.Running, StartedAt: time.Now().Add(-time.Minute)},
	}
	screen := render(cfg, snapshot{active: active}, -1, "", false, 160, 0)
	if n := strings.Count(screen, "⚠ stuck?"); n != 1 {
		t.Errorf("want exactly the stale review flagged, got %d flags:\n%s", n, screen)
	}
}

// The daemon badge has three states, not two: fresh is healthy, one poll interval late is a recoverable warning, and
// well past that it has stopped. The middle state is the one a binary badge hid.
func TestDaemonHealth(t *testing.T) {
	cfg := config.Config{PollInterval: time.Minute, WebAddr: "127.0.0.1:12377", UpdateRepo: "acme/prb"}
	for _, c := range []struct {
		age  time.Duration
		want string
	}{
		{30 * time.Second, "● watching (checked"},
		{90 * time.Second, "(late)"},
		{10 * time.Minute, "daemon not running"},
	} {
		h := header(cfg, snapshot{lastPoll: time.Now().Add(-c.age)}, false, 160, 0)
		if !strings.Contains(h, c.want) {
			t.Errorf("age %s: want %q in header:\n%s", c.age, c.want, h)
		}
	}
}

// d in the TUI queues a deep review, and the list marks deep reviews.
func TestQueueDeep(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/reviews.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	r := store.Review{Repo: "acme/api", Number: 7, Title: "Fix login", Status: store.Done}
	if msg := queue(st, r, true); msg != "queued a deep review of acme/api#7" {
		t.Errorf("queue = %q", msg)
	}
	latest, _ := st.Latest("acme/api", 7)
	if !latest.Deep || latest.Source != "manual" {
		t.Errorf("queued %+v", latest)
	}
	screen := render(config.Config{}, snapshot{recent: []store.Review{*latest}}, -1, "", false, 160, 0)
	if !strings.Contains(screen, "🔬") {
		t.Errorf("deep reviews are marked:\n%s", screen)
	}
}
