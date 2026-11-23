package store

import "testing"

func TestClearanceScored(t *testing.T) {
	// mid confidence with high PR and production risk → red, needs you.
	c := Review{Approval: 50, PRRisk: 8, ProdRisk: 9}.Clearance()
	if !c.Scored {
		t.Fatal("want scored")
	}
	if c.Score != 34 {
		t.Errorf("score = %d, want 34", c.Score)
	}
	if c.Band != "red" {
		t.Errorf("band = %q, want red", c.Band)
	}
	if len(c.Factors) != 3 {
		t.Fatalf("factors = %d, want 3", len(c.Factors))
	}
	// The breakdown must always sum to the headline, like the dispatch UI's legend.
	sum := 0
	for _, f := range c.Factors {
		sum += f.Earned
	}
	if sum != c.Score {
		t.Errorf("factor earned sum = %d, want %d", sum, c.Score)
	}
	if got := c.Factors[0].Band; got != "mid" {
		t.Errorf("confidence band = %q, want mid", got) // 50 → 5.0/10
	}
	if got := c.Factors[2].Band; got != "bad" {
		t.Errorf("prod-risk band = %q, want bad", got) // 9/10
	}
}

func TestClearanceGlance(t *testing.T) {
	// High confidence (a green pill) but medium risk → a glance, not a scary red. Confidence leads the weighting,
	// so the composite stays amber instead of being dragged below its own factor bands.
	c := Review{Approval: 82, PRRisk: 6, ProdRisk: 6}.Clearance()
	if c.Score != 63 {
		t.Errorf("score = %d, want 63", c.Score)
	}
	if c.Band != "amber" {
		t.Errorf("band = %q, want amber", c.Band)
	}
	if got := c.Factors[0].Band; got != "good" {
		t.Errorf("confidence band = %q, want good (82%% is a green pill)", got)
	}
}

func TestClearanceGreen(t *testing.T) {
	c := Review{Approval: 94, PRRisk: 1, ProdRisk: 0}.Clearance()
	if c.Score != 96 || c.Band != "green" {
		t.Errorf("got %d/%s, want 96/green", c.Score, c.Band)
	}
}

func TestClearanceUnscored(t *testing.T) {
	// Verifier hasn't rated risk: the score is the confidence alone, with just that one factor.
	c := Review{Approval: 80, PRRisk: -1, ProdRisk: -1}.Clearance()
	if c.Scored {
		t.Fatal("want unscored")
	}
	if c.Score != 80 {
		t.Errorf("score = %d, want 80 (confidence fallback)", c.Score)
	}
	if c.Band != "amber" {
		t.Errorf("band = %q, want amber", c.Band)
	}
	if len(c.Factors) != 1 {
		t.Errorf("factors = %d, want 1", len(c.Factors))
	}
}
