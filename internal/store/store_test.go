package store

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// Superseding closes only the same PR's older reviews that still wait for you.
func TestSupersede(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	add := func(repo string, number int, status string) int64 {
		id, err := s.Insert(&Review{Repo: repo, Number: number, Status: status})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	old := add("o/r", 1, AwaitingYou)
	done := add("o/r", 1, Done)
	other := add("o/r", 2, AwaitingYou)
	keep := add("o/r", 1, AwaitingYou)
	later := add("o/r", 1, AwaitingYou) // newer than keep: not replaced by it

	if err := s.Supersede("o/r", 1, keep); err != nil {
		t.Fatal(err)
	}
	want := map[int64]string{old: Done, done: Done, other: AwaitingYou, keep: AwaitingYou, later: AwaitingYou}
	for id, status := range want {
		if r, _ := s.Get(id); r.Status != status {
			t.Errorf("review %d: status %s, want %s", id, r.Status, status)
		}
	}
	if r, _ := s.Get(old); r.Decision != "superseded by review 4" {
		t.Errorf("superseded decision = %q", r.Decision)
	}
}

// A re-review is told about the last review in which every reviewer ran, not a skipped, failed or partial one.
func TestPreviousComplete(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	add := func(sha, status, tier, errText string) int64 {
		id, err := s.Insert(&Review{Repo: "o/r", Number: 1, HeadSHA: sha, Status: Running})
		if err != nil {
			t.Fatal(err)
		}
		_ = s.SetPlan(id, "", "", sha, tier, "", "", 0, 0, 0)
		if err := s.Finish(id, status, "", "", errText); err != nil {
			t.Fatal(err)
		}
		return id
	}
	if r, _ := s.PreviousComplete("o/r", 1, 1); r != nil {
		t.Fatal("no earlier reviews, want nil")
	}
	add("aaa", Done, "large", "")
	add("bbb", AwaitingYou, "large", "security: timed out") // partial
	add("ccc", Failed, "large", "fetch PR: 502")
	add("ddd", Ignored, "", "")
	now := add("eee", Running, "large", "")
	r, err := s.PreviousComplete("o/r", 1, now)
	if err != nil || r == nil || r.HeadSHA != "aaa" {
		t.Fatalf("got %+v, %v; want the review of aaa", r, err)
	}

	if err := s.AddRun(&Run{ReviewID: now, Agent: "verifier", Model: "opus", Findings: 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddRun(&Run{ReviewID: now, Agent: "security", Model: "opus", Turns: 14, Denials: 1, Checked: "read x"}); err != nil {
		t.Fatal(err)
	}
	runs, err := s.Runs(now)
	if err != nil || len(runs) != 2 || runs[0].Agent != "security" || runs[0].Denials != 1 || runs[1].Agent != "verifier" {
		t.Fatalf("runs = %+v, %v; want security then verifier", runs, err)
	}
}

// The breakdown counts only real (not dry-run) findings scored since the scoring changed, by certainty and severity.
func TestDecisions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	review := func(dry bool) int64 {
		id, err := s.Insert(&Review{Repo: "o/r", Number: 1, Status: Done, DryRun: dry})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	add := func(rid int64, conf int, sev, status string) {
		id, err := s.AddFinding(&Finding{ReviewID: rid, Agent: "logic", Path: "a", Line: 1, Severity: sev, Title: "t",
			Comment: "c", Status: Candidate})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateFinding(&Finding{ID: id, Line: 1, Comment: "c", Confidence: conf, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	live, dry := review(false), review(true)
	add(live, 92, "high", AutoPosted)
	add(live, 85, "low", Discarded)
	add(live, 88, "low", Posted)
	add(live, 65, "medium", Posted)
	add(live, 30, "high", Posted)
	add(live, 70, "high", Rejected) // never decided by you: not counted
	add(dry, 95, "high", WouldPost) // dry run: not counted
	add(dry, 50, "high", Discarded)

	since, rows, err := s.Decisions(80)
	if err != nil {
		t.Fatal(err)
	}
	if since.IsZero() {
		t.Error("scoring_since should be set when the database is opened")
	}
	want := []DecisionRow{
		{"≥ 80% · high", 1, 0, 0}, {"≥ 80% · medium", 0, 0, 0}, {"≥ 80% · low", 0, 1, 1},
		{"60–79%", 0, 1, 0}, {"40–59%", 0, 0, 0}, {"< 40%", 0, 1, 0},
	}
	for i, w := range want {
		if rows[i] != w {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], w)
		}
	}

	// Findings from reviews before the scoring changed are left out.
	if err := s.SetMeta("scoring_since", "2999-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, rows, _ := s.Decisions(80); rows[0].BotPosted != 0 || rows[2].Posted != 0 {
		t.Errorf("findings scored before scoring_since were counted: %+v", rows)
	}
}

// Design concerns carry no certainty: they get their own row, outside the certainty ranges.
func TestDecisionsDesignRow(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rid, _ := s.Insert(&Review{Repo: "o/r", Number: 1, Status: Done})
	for _, status := range []string{Posted, Discarded, Discarded} {
		id, _ := s.AddFinding(&Finding{ReviewID: rid, Agent: "design", Severity: "medium", Title: "t", Comment: "c", Status: Pending})
		_ = s.UpdateFinding(&Finding{ID: id, Comment: "c", Status: status})
	}
	_, rows, err := s.Decisions(80)
	if err != nil {
		t.Fatal(err)
	}
	last := rows[len(rows)-1]
	if last.Label != "design concerns" || last.Posted != 1 || last.Discarded != 2 {
		t.Errorf("design row = %+v", last)
	}
	if rows[5].Posted+rows[5].Discarded != 0 {
		t.Errorf("design concerns leaked into the < 40%% range: %+v", rows[5])
	}
}

// Rows from when the design reviewer was called "approach" are renamed when the database opens.
func TestOpenRenamesApproach(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reviews.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := s.Insert(&Review{Repo: "o/r", Number: 1, Status: Done})
	fid, _ := s.AddFinding(&Finding{ReviewID: rid, Agent: "approach", Severity: "low", Title: "t", Comment: "c", Status: Pending})
	_ = s.UpdateFinding(&Finding{ID: fid, Comment: "c", Verdict: "approach", Status: Posted})
	_ = s.AddRun(&Run{ReviewID: rid, Agent: "approach", Model: "opus"})
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fs, _ := s.Findings(rid)
	runs, _ := s.Runs(rid)
	if len(fs) != 1 || fs[0].Agent != "design" || fs[0].Verdict != "design" || len(runs) != 1 || runs[0].Agent != "design" {
		t.Errorf("not renamed: findings %+v, runs %+v", fs, runs)
	}
}

// Failures lists a PR only while its latest review is a recent failure, and never an ignored PR or a dry run.
func TestFailures(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	add := func(number int, status string, dry bool) int64 {
		id, err := s.Insert(&Review{Repo: "o/r", Number: number, Status: Running, DryRun: dry})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Finish(id, status, "", "", "fetch PR: 502"); err != nil {
			t.Fatal(err)
		}
		return id
	}
	failed := add(1, Failed, false)
	add(2, Failed, false)
	add(2, Done, false) // a later review of #2 went through
	add(3, Failed, false)
	_ = s.IgnorePR("o/r", 3)
	add(4, Failed, true)
	old := add(5, Failed, false)
	if _, err := s.db.Exec(`UPDATE reviews SET created_at = ? WHERE id = ?`,
		time.Now().Add(-10*24*time.Hour).UTC().Format(time.RFC3339), old); err != nil {
		t.Fatal(err)
	}

	got, err := s.Failures(time.Now().Add(-7 * 24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != failed {
		t.Errorf("got %+v, want only review %d (o/r#1)", got, failed)
	}
}

// Edit provenance: the bot's proposal is frozen when the verifier settles a finding, so a later human edit on post
// doesn't erase it. This is what lets us tell a finding posted word for word from one reworded.
func TestAuthoredCommentProvenance(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rev, err := s.Insert(&Review{Repo: "o/r", Number: 1, Status: AwaitingYou})
	if err != nil {
		t.Fatal(err)
	}

	// A reviewer proposes; the verifier refines the wording and settles it as Pending (both are bot writes).
	id, err := s.AddFinding(&Finding{ReviewID: rev, Agent: "logic", Severity: "low", Status: Candidate,
		Comment: "raw reviewer wording"})
	if err != nil {
		t.Fatal(err)
	}
	refined := "the bot's refined wording, as shown to you"
	if err := s.UpdateFinding(&Finding{ID: id, Status: Pending, Comment: refined}); err != nil {
		t.Fatal(err)
	}

	// You edit it and post it.
	if err := s.UpdateFinding(&Finding{ID: id, Status: Posted, Comment: "my own rewrite", PostedURL: "u"}); err != nil {
		t.Fatal(err)
	}
	f, _ := s.Finding(id)
	if f.AuthoredComment != refined {
		t.Errorf("authored = %q, want the bot's settled proposal %q", f.AuthoredComment, refined)
	}
	if f.Comment != "my own rewrite" {
		t.Errorf("comment = %q, want your edit", f.Comment)
	}
	if k := f.Edited(); k.String() != "edited" {
		t.Errorf("Edited() = %s, want edited", k)
	}

	// A finding posted without a change reads as verbatim.
	id2, _ := s.AddFinding(&Finding{ReviewID: rev, Agent: "logic", Severity: "low", Status: Candidate, Comment: "x"})
	_ = s.UpdateFinding(&Finding{ID: id2, Status: Pending, Comment: "the bot said this"})
	_ = s.UpdateFinding(&Finding{ID: id2, Status: Posted, Comment: "the bot said this", PostedURL: "u"})
	if f2, _ := s.Finding(id2); f2.Edited().String() != "verbatim" {
		t.Errorf("unchanged post: Edited() = %s, want verbatim", f2.Edited())
	}
}

// PostedEdits counts only the findings you posted yourself, split by whether you kept the bot's wording. The bot's
// auto-posts (always verbatim) and dry-run findings (posted nothing) are left out.
func TestPostedEdits(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	review := func(dry bool) int64 {
		id, err := s.Insert(&Review{Repo: "o/r", Number: 1, Status: Done, DryRun: dry})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	// post settles the bot's wording (a bot write) then records your decision with your final wording.
	post := func(rid int64, status, botWords, yourWords string) {
		id, err := s.AddFinding(&Finding{ReviewID: rid, Agent: "logic", Path: "a", Line: 1, Severity: "low",
			Title: "t", Comment: botWords, Status: Candidate})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateFinding(&Finding{ID: id, Line: 1, Comment: botWords, Status: Pending}); err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateFinding(&Finding{ID: id, Line: 1, Comment: yourWords, Status: status, PostedURL: "u"}); err != nil {
			t.Fatal(err)
		}
	}
	live, dry := review(false), review(true)
	post(live, Posted, "the bot said this", "the bot said this") // verbatim
	post(live, Posted, "keep it", "keep it")                     // verbatim
	post(live, Posted, "the bot said this", "my own rewrite")    // reworded
	post(live, AutoPosted, "auto one", "auto one")               // bot's auto-post: not counted
	post(live, Discarded, "dropped", "dropped")                  // discarded: not counted
	post(dry, Posted, "dry one", "dry one")                      // dry run: not counted

	e, err := s.PostedEdits()
	if err != nil {
		t.Fatal(err)
	}
	if want := (Edits{Verbatim: 2, Edited: 1, Unknown: 0}); e != want {
		t.Errorf("PostedEdits() = %+v, want %+v", e, want)
	}
	if e.Known() != 3 {
		t.Errorf("Known() = %d, want 3", e.Known())
	}
	if got := e.VerbatimShare(); got < 0.66 || got > 0.67 {
		t.Errorf("VerbatimShare() = %v, want ~0.667", got)
	}
}

// A review records which channel owns it, so the worker reviews it with that channel's subscription and settings; a
// review with no channel reads back empty (the default channel).
func TestReviewChannelRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.Insert(&Review{Repo: "acme/web", Number: 1, Status: Queued, Source: "auto", Channel: "company"})
	if err != nil {
		t.Fatal(err)
	}
	if r, err := s.Get(id); err != nil {
		t.Fatal(err)
	} else if r.Channel != "company" {
		t.Errorf("channel = %q, want company", r.Channel)
	}
	id2, _ := s.Insert(&Review{Repo: "me/x", Number: 2, Status: Queued})
	if r2, _ := s.Get(id2); r2.Channel != "" {
		t.Errorf("channel = %q, want empty for a review with no channel", r2.Channel)
	}
}

// ClaimNext pulls only untagged (default) reviews and those owned by the channels this instance runs, never
// another channel's — so a scoped instance can't run someone else's review under the wrong identity.
func TestClaimNextChannelScope(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mk := func(n int, channel string) {
		if _, err := s.Insert(&Review{Repo: "o/r", Number: n, Status: Queued, Channel: channel}); err != nil {
			t.Fatal(err)
		}
	}
	mk(1, "personal") // not ours
	mk(2, "")         // untagged: any instance may run
	mk(3, "company")  // ours

	// An instance that runs only "company" claims the untagged review and company's, in id order, never personal's.
	var got []int
	for {
		r, err := s.ClaimNext([]string{"company"})
		if err != nil {
			t.Fatal(err)
		}
		if r == nil {
			break
		}
		got = append(got, r.Number)
	}
	if want := []int{2, 3}; !slices.Equal(got, want) {
		t.Errorf("company instance claimed %v, want %v (never personal's #1)", got, want)
	}
	if r, _ := s.Get(1); r.Status != Queued {
		t.Errorf("personal review #1 = %q, want still Queued (left for its own instance)", r.Status)
	}
	// A default instance (empty names) claims only untagged reviews; #1 is tagged personal, so nothing is claimable.
	if r, err := s.ClaimNext(nil); err != nil || r != nil {
		t.Errorf("default instance claimed %v (err %v), want nothing (only #1 left, and it is tagged)", r, err)
	}
}

// OpenMemory gives an ephemeral store: a review round-trips within it, and a second one is independent — it keeps
// nothing on disk, for a one-shot CI run.
func TestOpenMemory(t *testing.T) {
	s, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.Insert(&Review{Repo: "o/r", Number: 1, Status: Queued})
	if err != nil {
		t.Fatal(err)
	}
	if r, err := s.Get(id); err != nil || r.Repo != "o/r" {
		t.Fatalf("round-trip in memory: r=%+v err=%v", r, err)
	}
	s2, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if r, _ := s2.Get(id); r != nil {
		t.Error("a second in-memory store should be independent, but found the first store's review")
	}
}

// PostedToCheck yields the posted inline findings whose downstream state is due, and is bounded: a finding drops out
// once its PR is merged or closed, once its review predates `since`, or when it was checked since `before`. Discarded,
// pending, non-inline and dry-run findings are never in it.
func TestPostedToCheck(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	post := func(rev int64, status string, inline bool, line int) int64 {
		id, err := s.AddFinding(&Finding{ReviewID: rev, Agent: "logic", Path: "a.go", Line: line, Severity: "low",
			Title: "t", Comment: "c", Status: Candidate, Inline: inline})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateFinding(&Finding{ID: id, Path: "a.go", Line: line, Comment: "c", Status: status,
			Inline: inline, PostedURL: "u"}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	open, _ := s.Insert(&Review{Repo: "o/r", Number: 1, Status: Done})
	merged, _ := s.Insert(&Review{Repo: "o/r", Number: 2, Status: Done})
	dry, _ := s.Insert(&Review{Repo: "o/r", Number: 3, Status: Done, DryRun: true})

	wantA := post(open, Posted, true, 10)
	wantB := post(open, AutoPosted, true, 11)
	post(open, Posted, false, 0)    // non-inline (design): no thread to read
	post(open, Discarded, true, 12) // never posted
	post(open, Pending, true, 13)   // not posted yet
	onMerged := post(merged, Posted, true, 10)
	post(dry, Posted, true, 10) // dry run posts nothing
	if err := s.SetPRStatus("o/r", 2, PRStatus{State: PRMerged}); err != nil {
		t.Fatal(err)
	}

	const before, since = "3000-01-01T00:00:00Z", "2000-01-01T00:00:00Z"
	due, err := s.PostedToCheck(before, since, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]PostedFinding{}
	for _, pf := range due {
		got[pf.ID] = pf
	}
	if _, a := got[wantA]; !a {
		t.Errorf("open PR's posted finding %d is missing", wantA)
	}
	if _, b := got[wantB]; !b {
		t.Errorf("open PR's auto-posted finding %d is missing", wantB)
	}
	if _, m := got[onMerged]; m || len(got) != 2 {
		t.Fatalf("due = %v, want exactly %d and %d (the merged PR's %d must drop out)", keysOf(got), wantA, wantB, onMerged)
	}
	if pf := got[wantA]; pf.Repo != "o/r" || pf.Number != 1 || !pf.Inline || pf.Comment != "c" {
		t.Errorf("carried the wrong shape for MatchThread: %+v", pf)
	}

	// Age cap: a `since` in the future makes every review too old, so nothing is due.
	if none, _ := s.PostedToCheck(before, "3999-01-01T00:00:00Z", 100); len(none) != 0 {
		t.Errorf("age cap ignored: %d still due", len(none))
	}
	// Staleness: a finding checked after `before` waits; one never checked is still due.
	if err := s.SetDownstream(wantA, "endorsed", "2500-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for _, pf := range mustCheck(t, s, "2400-01-01T00:00:00Z", since) {
		if pf.ID == wantA {
			t.Error("a finding checked more recently than `before` should wait its turn")
		}
	}
}

// SetDownstream records a signal, and an unknown result never erases a signal already seen — it only moves the check
// time forward.
func TestSetDownstream(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rev, _ := s.Insert(&Review{Repo: "o/r", Number: 1, Status: Done})
	id, _ := s.AddFinding(&Finding{ReviewID: rev, Agent: "logic", Path: "a.go", Line: 1, Severity: "low", Title: "t",
		Comment: "c", Status: Candidate, Inline: true})
	_ = s.UpdateFinding(&Finding{ID: id, Path: "a.go", Line: 1, Comment: "c", Status: Posted, Inline: true, PostedURL: "u"})

	if err := s.SetDownstream(id, "endorsed", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if f, _ := s.Finding(id); f.Downstream != "endorsed" {
		t.Fatalf("downstream = %q, want endorsed", f.Downstream)
	}
	// unknown must not clobber the endorsement, but should move the check time on.
	if err := s.SetDownstream(id, "unknown", "2026-02-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if f, _ := s.Finding(id); f.Downstream != "endorsed" {
		t.Errorf("an unknown result erased the signal: now %q, want endorsed kept", f.Downstream)
	} else if !f.DownstreamAt.Equal(parseTime("2026-02-02T00:00:00Z")) {
		t.Errorf("check time not moved on: %v", f.DownstreamAt)
	}
	// a real signal does overwrite.
	if err := s.SetDownstream(id, "disputed", "2026-03-03T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if f, _ := s.Finding(id); f.Downstream != "disputed" {
		t.Errorf("downstream = %q, want disputed", f.Downstream)
	}
}

// DownstreamMix counts posted findings by what became of them; dry-run and non-inline findings are left out.
func TestDownstreamMix(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rev, _ := s.Insert(&Review{Repo: "o/r", Number: 1, Status: Done})
	post := func(signal string) {
		id, _ := s.AddFinding(&Finding{ReviewID: rev, Agent: "logic", Path: "a.go", Line: 1, Severity: "low",
			Title: "t", Comment: "c", Status: Candidate, Inline: true})
		_ = s.UpdateFinding(&Finding{ID: id, Path: "a.go", Line: 1, Comment: "c", Status: Posted, Inline: true, PostedURL: "u"})
		if signal != "" {
			_ = s.SetDownstream(id, signal, "2026-01-01T00:00:00Z")
		}
	}
	post("endorsed")
	post("endorsed")
	post("disputed")
	post("quiet")
	post("") // unchecked

	m, err := s.DownstreamMix()
	if err != nil {
		t.Fatal(err)
	}
	if want := (DownstreamMix{Endorsed: 2, Quiet: 1, Disputed: 1, Unchecked: 1}); m != want {
		t.Errorf("DownstreamMix = %+v, want %+v", m, want)
	}
	if m.Known() != 4 {
		t.Errorf("Known = %d, want 4", m.Known())
	}
}

func keysOf(m map[int64]PostedFinding) []int64 {
	var ks []int64
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func mustCheck(t *testing.T, s *Store, before, since string) []PostedFinding {
	t.Helper()
	due, err := s.PostedToCheck(before, since, 100)
	if err != nil {
		t.Fatal(err)
	}
	return due
}

// PendingByReview counts only the findings still waiting for you, per review, and reports the top severity among them.
func TestPendingByReview(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r1, _ := s.Insert(&Review{Repo: "o/r", Number: 1, Status: AwaitingYou})
	r2, _ := s.Insert(&Review{Repo: "o/r", Number: 2, Status: AwaitingYou})
	add := func(rid int64, sev, status string) {
		if _, err := s.AddFinding(&Finding{ReviewID: rid, Agent: "logic", Path: "a", Line: 1, Severity: sev, Title: "t",
			Comment: "c", Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	add(r1, "low", Pending)
	add(r1, "high", Pending)
	add(r1, "medium", Posted) // already posted: no longer waiting
	add(r2, "medium", Pending)
	add(r2, "high", Rejected) // the verifier threw it out: not waiting

	got, err := s.PendingByReview()
	if err != nil {
		t.Fatal(err)
	}
	if w := got[r1]; w.Count != 2 || w.TopSeverity != "high" {
		t.Errorf("r1 = %+v, want {Count:2 TopSeverity:high}", w)
	}
	if w := got[r2]; w.Count != 1 || w.TopSeverity != "medium" {
		t.Errorf("r2 = %+v, want {Count:1 TopSeverity:medium}", w)
	}
	if _, ok := got[999]; ok {
		t.Errorf("a review with no pending findings should be absent, got %+v", got[999])
	}
}
