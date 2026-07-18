// Package trust computes a device's reputation weight from its reporting
// history. It is pure, no I/O: callers gather the inputs (report count,
// device age, disagreement count) from storage and pass them in.
package trust

// Tunable constants controlling the device trust formula.
const (
	TrustBase = 0.5
	TrustMin  = 0.0
	TrustMax  = 3.0

	tenureMaxDays  = 30.0
	tenureWeight   = 0.5
	volumeMaxCount = 20
	volumeWeight   = 0.05
	penaltyWeight  = 0.25
)

// Compute returns a device's trust_weight from its history:
//   - tenure rewards account age, capping out at +0.5 once the device is at
//     least tenureMaxDays old.
//   - volume rewards reporting history, capping out at +1.0 at
//     volumeMaxCount or more reports.
//   - penalty subtracts for each report contradicted by the community/admin
//     outcome.
//
// Negative reportCount, ageDays, or disagreements are treated as zero. The
// result is clamped to [TrustMin, TrustMax].
func Compute(reportCount int, ageDays float64, disagreements int) float64 {
	if reportCount < 0 {
		reportCount = 0
	}
	if ageDays < 0 {
		ageDays = 0
	}
	if disagreements < 0 {
		disagreements = 0
	}

	tenure := ageDays / tenureMaxDays
	if tenure > 1.0 {
		tenure = 1.0
	}
	tenure *= tenureWeight

	volume := reportCount
	if volume > volumeMaxCount {
		volume = volumeMaxCount
	}

	penalty := float64(disagreements) * penaltyWeight

	trust := TrustBase + tenure + float64(volume)*volumeWeight - penalty

	if trust < TrustMin {
		trust = TrustMin
	}
	if trust > TrustMax {
		trust = TrustMax
	}
	return trust
}
