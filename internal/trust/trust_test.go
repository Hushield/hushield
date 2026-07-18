package trust

import "testing"

func TestCompute(t *testing.T) {
	tests := []struct {
		name          string
		reportCount   int
		ageDays       float64
		disagreements int
		want          float64
	}{
		{"brand new device", 0, 0, 0, 0.5},
		{"seasoned device", 20, 30, 0, 2.0},
		{"heavy penalty floors at zero", 0, 0, 10, 0.0},
		{"negative inputs clamped to base", -5, -10, -3, 0.5},
		{"partial tenure and volume", 10, 15, 0, 0.5 + 0.25 + 0.5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Compute(tc.reportCount, tc.ageDays, tc.disagreements)
			const epsilon = 1e-9
			if diff := got - tc.want; diff < -epsilon || diff > epsilon {
				t.Errorf("Compute(%d, %v, %d) = %v, want %v", tc.reportCount, tc.ageDays, tc.disagreements, got, tc.want)
			}
		})
	}
}

func TestCompute_ClampsToRange(t *testing.T) {
	if got := Compute(1000, 1000, 0); got > TrustMax {
		t.Errorf("Compute with huge inputs = %v, want <= %v", got, TrustMax)
	}
	if got := Compute(0, 0, 1000); got < TrustMin {
		t.Errorf("Compute with huge penalty = %v, want >= %v", got, TrustMin)
	}
}
