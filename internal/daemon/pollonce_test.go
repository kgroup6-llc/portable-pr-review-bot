package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/badam1/personal-review-bot/internal/config"
)

// poll-once fails fast without a data store: a scheduled cycle with no idempotency would re-review and re-post every
// open PR on every run, so it refuses rather than quietly burning a review (and tokens) per cycle.
func TestPollOnceNeedsDataRepo(t *testing.T) {
	err := PollOnce(context.Background(), config.Config{})
	if err == nil || !strings.Contains(err.Error(), "DATA_REPO") {
		t.Fatalf("PollOnce without DATA_REPO: err = %v, want one mentioning DATA_REPO", err)
	}
}
