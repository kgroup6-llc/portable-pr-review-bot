package store

import (
	"fmt"
	"math"
	"time"
)

// Clearance weights the verifier's signals into one 0-100 "safe to approve on sight" score, so the inbox can lead
// with a single number instead of a wall of text. Production impact counts most; low confidence and PR risk pull it
// down. It mirrors the dispatch UI one-for-one: each factor fills a slot sized by its weight, and the score is the
// points earned across every slot (so the per-factor breakdown always sums to the headline).

// scoreWeights are the factor weights; they sum to 1. Production blast-radius dominates, confidence gates.
var scoreWeights = struct{ Conf, PR, Prod float64 }{Conf: 0.30, PR: 0.25, Prod: 0.45}

// Factor is one input to the score: the weight it carries and the points it earned toward the total.
type Factor struct {
	Key    string  // conf | pr | prod
	Label  string  // confidence | PR risk | prod risk
	Weight float64 // 0..1
	Earned int     // points toward the score, 0..Max
	Max    int     // round(Weight*100): the most this factor can contribute
	Band   string  // good | mid | bad — this factor on its own
}

// Clearance is a review's weighted score and the breakdown behind it.
type Clearance struct {
	Score   int      // 0..100
	Band    string   // green | amber | red
	Scored  bool     // false when the verifier hasn't rated risk yet (score falls back to confidence alone)
	Factors []Factor // confidence, PR risk, prod risk — or just confidence when not yet scored
}

// scoreBand maps a 0-100 score to its lane: green clears on sight, amber wants a glance, red needs you.
func scoreBand(s int) string {
	switch {
	case s >= 85:
		return "green"
	case s >= 60:
		return "amber"
	default:
		return "red"
	}
}

// factorBand rates one factor good/mid/bad on the 0-10 scale. higher=true means a bigger number is better
// (confidence); higher=false means a bigger number is worse (risk).
func factorBand(n float64, higher bool) string {
	if higher {
		switch {
		case n >= 8:
			return "good"
		case n >= 5:
			return "mid"
		default:
			return "bad"
		}
	}
	switch {
	case n <= 3:
		return "good"
	case n <= 6:
		return "mid"
	default:
		return "bad"
	}
}

func factorPoints(weight, scale float64) int { return int(math.Round(weight * scale)) }

// WeightPct is the factor's slot width in the breakdown bar: its share of the 100-point score.
func (f Factor) WeightPct() int { return int(math.Round(f.Weight * 100)) }

// FillPct is how full the factor's slot is — the points earned out of the most it could contribute.
func (f Factor) FillPct() int {
	if f.Max == 0 {
		return 0
	}
	return int(math.Round(float64(f.Earned) / float64(f.Max) * 100))
}

// WeightLabel formats the factor's weight like the dispatch legend (×0.30).
func (f Factor) WeightLabel() string { return fmt.Sprintf("%.2f", f.Weight) }

// ConfBand, PRBand and ProdBand rate the component pills on the card as good / mid / bad.
func (r Review) ConfBand() string { return factorBand(float64(r.Approval)/10, true) }
func (r Review) PRBand() string   { return factorBand(float64(r.PRRisk), false) }
func (r Review) ProdBand() string { return factorBand(float64(r.ProdRisk), false) }

// Took is the review's wall-clock duration, for the "time" pill and its band.
func (r Review) Took() time.Duration { return r.FinishedAt.Sub(r.StartedAt) }

// Clearance computes the review's weighted score. When the verifier hasn't scored risk yet (PRRisk or ProdRisk < 0),
// the score is the verifier's confidence alone and the breakdown shows only that factor.
func (r Review) Clearance() Clearance {
	conf := Factor{
		Key: "conf", Label: "confidence", Weight: scoreWeights.Conf,
		Earned: factorPoints(scoreWeights.Conf, float64(r.Approval)),
		Max:    factorPoints(scoreWeights.Conf, 100),
		Band:   factorBand(float64(r.Approval)/10, true),
	}
	if r.PRRisk < 0 || r.ProdRisk < 0 {
		return Clearance{Score: r.Approval, Band: scoreBand(r.Approval), Scored: false, Factors: []Factor{conf}}
	}
	pr := Factor{
		Key: "pr", Label: "PR risk", Weight: scoreWeights.PR,
		Earned: factorPoints(scoreWeights.PR, float64(100-r.PRRisk*10)),
		Max:    factorPoints(scoreWeights.PR, 100),
		Band:   factorBand(float64(r.PRRisk), false),
	}
	prod := Factor{
		Key: "prod", Label: "prod risk", Weight: scoreWeights.Prod,
		Earned: factorPoints(scoreWeights.Prod, float64(100-r.ProdRisk*10)),
		Max:    factorPoints(scoreWeights.Prod, 100),
		Band:   factorBand(float64(r.ProdRisk), false),
	}
	score := conf.Earned + pr.Earned + prod.Earned
	return Clearance{Score: score, Band: scoreBand(score), Scored: true, Factors: []Factor{conf, pr, prod}}
}
