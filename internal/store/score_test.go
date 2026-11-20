package store

import "testing"

func TestClearanceScored(t *testing.T) {
	// payments-like: decent confidence but high PR and production risk → red, needs you.
	c := Review{Approval: 78, PRRisk: 7, ProdRisk: 8}.Clearance()
	if !c.Scored {
		t.Fatal("want scored")
	}
	if c.Score != 40 {
		t.Errorf("score = %d, want 40", c.Score)
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
		t.Errorf("confidence band = %q, want mid", got) // 78 → 7.8/10
	}
	if got := c.Factors[2].Band; got != "bad" {
		t.Errorf("prod-risk band = %q, want bad", got) // 8/10
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
