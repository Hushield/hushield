package scoring

import (
	"math"
	"testing"
	"time"
)

// fixed reference time so tests are deterministic
var testNow = time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

const eps = 1e-9

func approxEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) <= epsilon
}

// newReport builds a Report whose CreatedAt is ageDays before testNow.
// A negative ageDays produces a future-dated report.
func newReport(vote Vote, cat Category, trust float64, ageDays float64) Report {
	offset := time.Duration(ageDays * 24 * float64(time.Hour))
	return Report{
		Category:    cat,
		Vote:        vote,
		DeviceTrust: trust,
		CreatedAt:   testNow.Add(-offset),
	}
}

func TestScore(t *testing.T) {
	tests := []struct {
		name            string
		reports         []Report
		override        Override
		neighborSpoof   bool
		wantStatus      Status
		wantScore       float64
		wantTopCategory Category
	}{
		{
			name:            "no reports, no override, no spoof",
			reports:         nil,
			wantStatus:      StatusUnknown,
			wantScore:       0,
			wantTopCategory: "",
		},
		{
			name:            "no reports plus neighbor spoof",
			reports:         nil,
			neighborSpoof:   true,
			wantStatus:      StatusSuspected,
			wantScore:       2.0,
			wantTopCategory: "",
		},
		{
			name: "several fresh scam reports crossing block threshold",
			reports: []Report{
				newReport(VoteSpam, CategoryScam, 1.0, 0),
				newReport(VoteSpam, CategoryScam, 1.0, 0),
				newReport(VoteSpam, CategoryScam, 1.0, 0),
			},
			wantStatus:      StatusBlocked,
			wantScore:       6.0,
			wantTopCategory: CategoryScam,
		},
		{
			name: "score between thresholds is suspected",
			reports: []Report{
				newReport(VoteSpam, CategoryScam, 1.0, 0),
				newReport(VoteSpam, CategoryScam, 1.0, 0),
			},
			wantStatus:      StatusSuspected,
			wantScore:       4.0,
			wantTopCategory: CategoryScam,
		},
		{
			name: "category weighting: fresh scam scores 2.0",
			reports: []Report{
				newReport(VoteSpam, CategoryScam, 1.0, 0),
			},
			wantStatus:      StatusSuspected,
			wantScore:       2.0,
			wantTopCategory: CategoryScam,
		},
		{
			name: "category weighting: fresh other scores 0.75",
			reports: []Report{
				newReport(VoteSpam, CategoryOther, 1.0, 0),
			},
			wantStatus:      StatusUnknown,
			wantScore:       0.75,
			wantTopCategory: CategoryOther,
		},
		{
			name: "decay: report exactly HalfLifeDays old contributes half",
			reports: []Report{
				newReport(VoteSpam, CategoryScam, 1.0, HalfLifeDays),
			},
			wantStatus:      StatusUnknown,
			wantScore:       1.0, // half of the fresh 2.0
			wantTopCategory: CategoryScam,
		},
		{
			name: "decay: very old report (365d) contributes ~0",
			reports: []Report{
				newReport(VoteSpam, CategoryScam, 1.0, 365),
			},
			wantStatus:      StatusUnknown,
			wantScore:       0, // checked with a looser epsilon below
			wantTopCategory: CategoryScam,
		},
		{
			name: "counter-reports drive a suspected number back to unknown",
			reports: []Report{
				newReport(VoteSpam, CategoryScam, 1.0, 0),
				newReport(VoteSpam, CategoryScam, 1.0, 0),
				newReport(VoteNotSpam, CategoryOther, 1.0, 0),
				newReport(VoteNotSpam, CategoryOther, 1.0, 0),
				newReport(VoteNotSpam, CategoryOther, 1.0, 0),
			},
			wantStatus:      StatusUnknown,
			wantScore:       1.0, // 4.0 - 3.0
			wantTopCategory: CategoryScam,
		},
		{
			name: "score floored at 0 when counters exceed spam",
			reports: []Report{
				newReport(VoteSpam, CategoryScam, 1.0, 0),
				newReport(VoteNotSpam, CategoryOther, 1.0, 0),
				newReport(VoteNotSpam, CategoryOther, 1.0, 0),
				newReport(VoteNotSpam, CategoryOther, 1.0, 0),
				newReport(VoteNotSpam, CategoryOther, 1.0, 0),
				newReport(VoteNotSpam, CategoryOther, 1.0, 0),
			},
			wantStatus: StatusUnknown,
			wantScore:  0,
			// the lone spam report still tallies a positive scam contribution
			wantTopCategory: CategoryScam,
		},
		{
			name: "future-dated report treated as age 0, not amplified",
			reports: []Report{
				newReport(VoteSpam, CategoryScam, 1.0, -10), // 10 days in the future
			},
			wantStatus:      StatusSuspected,
			wantScore:       2.0,
			wantTopCategory: CategoryScam,
		},
		{
			name: "device trust 0 contributes nothing",
			reports: []Report{
				newReport(VoteSpam, CategoryScam, 0, 0),
			},
			wantStatus:      StatusUnknown,
			wantScore:       0,
			wantTopCategory: "",
		},
		{
			name: "device trust clamps at TrustMax",
			reports: []Report{
				newReport(VoteSpam, CategoryScam, 10.0, 0), // above TrustMax, should clamp to 5.0
			},
			wantStatus:      StatusBlocked,
			wantScore:       10.0, // 5.0(clamped trust) * 1(decay) * 2.0(scam mult)
			wantTopCategory: CategoryScam,
		},
		{
			name: "device trust exactly TrustMax matches clamped-above case",
			reports: []Report{
				newReport(VoteSpam, CategoryScam, 5.0, 0),
			},
			wantStatus:      StatusBlocked,
			wantScore:       10.0,
			wantTopCategory: CategoryScam,
		},
		{
			name: "override allow beats a blocked-level score",
			reports: []Report{
				newReport(VoteSpam, CategoryScam, 1.0, 0),
				newReport(VoteSpam, CategoryScam, 1.0, 0),
				newReport(VoteSpam, CategoryScam, 1.0, 0),
			},
			override:        OverrideAllow,
			wantStatus:      StatusAllowlisted,
			wantScore:       6.0,
			wantTopCategory: CategoryScam,
		},
		{
			name:            "override block on zero score",
			reports:         nil,
			override:        OverrideBlock,
			wantStatus:      StatusOverriddenBlock,
			wantScore:       0,
			wantTopCategory: "",
		},
		{
			name: "TopCategory tie-break prefers scam over other",
			reports: []Report{
				// contrib = 0.75 * 1(decay) * 2.0(scam mult) = 1.5
				newReport(VoteSpam, CategoryScam, 0.75, 0),
				// contrib = 2.0 * 1(decay) * 0.75(other mult) = 1.5
				newReport(VoteSpam, CategoryOther, 2.0, 0),
			},
			wantStatus:      StatusSuspected,
			wantScore:       3.0,
			wantTopCategory: CategoryScam,
		},
		{
			name: "unknown category is treated as other for weight and TopCategory",
			reports: []Report{
				newReport(VoteSpam, Category("smishing"), 1.0, 0),
			},
			wantStatus:      StatusUnknown,
			wantScore:       0.75,
			wantTopCategory: CategoryOther,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := Input{
				Reports:       tt.reports,
				Override:      tt.override,
				NeighborSpoof: tt.neighborSpoof,
				Now:           testNow,
			}
			got := Score(in)

			if got.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", got.Status, tt.wantStatus)
			}
			if got.TopCategory != tt.wantTopCategory {
				t.Errorf("TopCategory = %q, want %q", got.TopCategory, tt.wantTopCategory)
			}

			wantEps := eps
			if tt.name == "decay: very old report (365d) contributes ~0" {
				wantEps = 0.001 // sub-threshold tolerance for the near-zero decay case
			}
			if !approxEqual(got.Score, tt.wantScore, wantEps) {
				t.Errorf("Score = %v, want %v (eps %v)", got.Score, tt.wantScore, wantEps)
			}
			if got.Score < 0 {
				t.Errorf("Score must never be negative, got %v", got.Score)
			}
		})
	}
}

func TestDecay(t *testing.T) {
	if !approxEqual(Decay(0), 1.0, eps) {
		t.Errorf("Decay(0) = %v, want 1", Decay(0))
	}
	if !approxEqual(Decay(HalfLifeDays), 0.5, eps) {
		t.Errorf("Decay(HalfLifeDays) = %v, want 0.5", Decay(HalfLifeDays))
	}
	if got := Decay(365); got >= 0.001 {
		t.Errorf("Decay(365) = %v, want a value close to 0", got)
	}
	if got := Decay(365); got < 0 {
		t.Errorf("Decay(365) = %v, must not be negative", got)
	}
}
