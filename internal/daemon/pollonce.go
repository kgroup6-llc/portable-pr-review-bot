package daemon

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/badam1/personal-review-bot/internal/config"
	"github.com/badam1/personal-review-bot/internal/gh"
)

// PollOnce runs a single poll cycle statelessly and returns: it finds the PRs this instance's channels are asked to
// review and reviews each one in an in-memory store, leaving nothing on disk. It is the scheduled-agent / cron
// counterpart to Run (the long-running poller) — pull-based and outbound-only (no webhook listener, so no inbound
// surface), the same reviewDeps and the same per-channel identity.
//
// Idempotency across cycles comes from the git data store: the pipeline skips a SHA it has already recorded, so a
// repeated cycle does not re-review (and re-post) a PR it already handled. DATA_REPO is therefore required — without
// it every cycle would re-review every open PR. (Correct on a single persistent box today; fresh-runner / multi-runner
// safety needs the data store's fetch-pull + atomic writes, tracked in the datastore hardening issue.)
func PollOnce(ctx context.Context, cfg config.Config) error {
	if cfg.DataRepoDir == "" {
		return fmt.Errorf("poll-once needs a data store for idempotency: set DATA_REPO, " +
			"otherwise every run re-reviews and re-posts every open PR")
	}
	client, err := gh.New()
	if err != nil {
		return fmt.Errorf("poll-once polls for PRs where your review is requested, so it needs a GitHub login: %w", err)
	}
	base, err := reviewDeps(ctx, cfg, client)
	if err != nil {
		return err
	}
	prs, err := requested(base)
	if err != nil {
		return fmt.Errorf("poll: %w", err)
	}
	now := time.Now()
	reviewed := 0
	for _, pr := range prs {
		if err := ctx.Err(); err != nil { // interrupted: stop cleanly between PRs
			return err
		}
		if pr.Draft && cfg.SkipDrafts {
			continue
		}
		channel, owned := base.RouteChannel(pr.Repo)
		if !owned {
			continue // no channel this instance runs owns this repo
		}
		if idle(pr, cfg.MaxAge, now) {
			continue // untouched for MaxAge; a scheduled agent shouldn't keep re-checking it
		}
		if err := reviewPROnce(ctx, base, pr.Repo, pr.Number, pr.HeadSHA, channel, "poll"); err != nil {
			log.Printf("poll-once: %s#%d: %v", pr.Repo, pr.Number, err)
			continue
		}
		reviewed++
	}
	log.Printf("poll-once: %d requested, %d reviewed (the rest up to date, draft, idle, or not owned)", len(prs), reviewed)
	return nil
}
