// Package scoring implements the pure spam-scoring engine: given a phone
// number's community reports (and any manual override), it decides the
// number's Status, its dominant spam Category, and a numeric Score.
//
// This package has no database and no I/O. It is deterministic: the same
// Input always produces the same Result, and no clock is read internally —
// callers must pass Now explicitly.
package scoring

import (
	"math"
	"time"
)

// Category classifies why a report was filed.
type Category string

const (
	CategoryScam         Category = "scam"
	CategoryRobocall     Category = "robocall"
	CategoryTelemarketer Category = "telemarketer"
	CategoryOther        Category = "other"
)

// Vote is the reporter's judgment on a phone number.
type Vote string

const (
	VoteSpam    Vote = "spam"
	VoteNotSpam Vote = "not_spam"
)

// Override is a manual admin decision that supersedes the community score.
type Override string

const (
	OverrideNone  Override = ""
	OverrideAllow Override = "allow"
	OverrideBlock Override = "block"
)

// Status is the final classification of a phone number.
type Status string

const (
	StatusUnknown         Status = "unknown"
	StatusSuspected       Status = "suspected"
	StatusBlocked         Status = "blocked"
	StatusAllowlisted     Status = "allowlisted"
	StatusOverriddenBlock Status = "overridden_block"
)

// Report is a single community report about a phone number.
type Report struct {
	Category    Category
	Vote        Vote
	DeviceTrust float64 // per-device trust_weight (from devices.trust_weight)
	CreatedAt   time.Time
}

// Input is everything Score needs to evaluate a phone number.
type Input struct {
	Reports       []Report
	Override      Override
	NeighborSpoof bool // true when this number spoofs the querying caller's NPA-NXX (serve-time only)
	Now           time.Time
}

// Result is the outcome of scoring a phone number.
type Result struct {
	Status      Status
	TopCategory Category // dominant spam category by weighted contribution; "" if no spam contribution
	Score       float64  // final non-negative score
}

// Tunable constants controlling the scoring algorithm.
const (
	BaseWeight        = 1.0
	CounterMultiplier = 1.0 // weight subtracted per not_spam report

	HalfLifeDays = 30.0

	NeighborSpoofBonus = 2.0

	SuspectThreshold = 2.0
	BlockThreshold   = 5.0

	TrustMin = 0.0
	TrustMax = 5.0
)

// categoryMultipliers weights each spam category's contribution to the
// score. An unrecognized/unknown Category is treated as CategoryOther.
var categoryMultipliers = map[Category]float64{
	CategoryScam:         2.0,
	CategoryRobocall:     1.5,
	CategoryTelemarketer: 1.0,
	CategoryOther:        0.75,
}

// categoryPriority orders categories for TopCategory tie-breaking, matching
// the descending order of their multipliers (scam > robocall > telemarketer
// > other).
var categoryPriority = []Category{
	CategoryScam,
	CategoryRobocall,
	CategoryTelemarketer,
	CategoryOther,
}

// categoryMultiplier returns the weighting multiplier for cat, treating any
// category outside the known set as CategoryOther.
func categoryMultiplier(cat Category) float64 {
	if m, ok := categoryMultipliers[cat]; ok {
		return m
	}
	return categoryMultipliers[CategoryOther]
}

// normalizedCategory maps cat to one of the known categories, treating any
// unrecognized category as CategoryOther.
func normalizedCategory(cat Category) Category {
	if _, ok := categoryMultipliers[cat]; ok {
		return cat
	}
	return CategoryOther
}

// Decay returns the exponential half-life decay factor for a report that is
// ageDays old: Decay(0) == 1, Decay(HalfLifeDays) == 0.5, and so on.
func Decay(ageDays float64) float64 {
	return math.Pow(0.5, ageDays/HalfLifeDays)
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// Score evaluates a phone number's community reports (and any override)
// into a final Result.
func Score(in Input) Result {
	score := 0.0
	categoryTally := make(map[Category]float64)

	for _, r := range in.Reports {
		ageDays := in.Now.Sub(r.CreatedAt).Hours() / 24
		if ageDays < 0 {
			ageDays = 0
		}
		d := Decay(ageDays)
		trust := clamp(r.DeviceTrust, TrustMin, TrustMax)
		contrib := BaseWeight * trust * d

		if r.Vote == VoteSpam {
			m := categoryMultiplier(r.Category)
			weighted := contrib * m
			score += weighted
			categoryTally[normalizedCategory(r.Category)] += weighted
		} else {
			score -= contrib * CounterMultiplier
		}
	}

	if in.NeighborSpoof {
		score += NeighborSpoofBonus
	}

	if score < 0 {
		score = 0
	}

	topCategory := Category("")
	bestTally := 0.0
	for _, cat := range categoryPriority {
		tally := categoryTally[cat]
		if tally > bestTally {
			bestTally = tally
			topCategory = cat
		}
	}

	var status Status
	switch {
	case in.Override == OverrideAllow:
		status = StatusAllowlisted
	case in.Override == OverrideBlock:
		status = StatusOverriddenBlock
	case score >= BlockThreshold:
		status = StatusBlocked
	case score >= SuspectThreshold:
		status = StatusSuspected
	default:
		status = StatusUnknown
	}

	return Result{
		Status:      status,
		TopCategory: topCategory,
		Score:       score,
	}
}
