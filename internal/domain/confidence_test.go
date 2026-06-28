package domain

import (
	"testing"
	"time"
)

func TestWilsonConfidenceThresholds(t *testing.T) {
	tests := []struct {
		name      string
		evidence  int
		total     int
		wantLower float64
		wantUpper float64
	}{
		{name: "single evidence remains conservative", evidence: 1, total: 1, wantLower: 0.37, wantUpper: 0.39},
		{name: "two clean evidence is borderline", evidence: 2, total: 2, wantLower: 0.54, wantUpper: 0.56},
		{name: "three clean evidence crosses", evidence: 3, total: 3, wantLower: 0.64, wantUpper: 0.66},
		{name: "contradiction lowers score", evidence: 3, total: 4, wantLower: 0.42, wantUpper: 0.44},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WilsonLower(tt.evidence, tt.total)
			if got < tt.wantLower || got > tt.wantUpper {
				t.Fatalf("WilsonLower(%d,%d) = %.3f, want in [%.2f, %.2f]", tt.evidence, tt.total, got, tt.wantLower, tt.wantUpper)
			}
		})
	}
}

func TestCandidateConfidenceStatus(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	confidence := CandidateConfidence(now, 3, 0, []string{"cr_a", "cr_b"}, []string{"t_a"}, now)
	if confidence < DefaultUnderReviewThreshold {
		t.Fatalf("confidence %.3f should cross %.2f", confidence, DefaultUnderReviewThreshold)
	}
	if status := CandidateStatus(confidence, 3, 0); status != "under_review" {
		t.Fatalf("status = %s, want under_review", status)
	}

	withContradiction := CandidateConfidence(now, 3, 1, []string{"cr_a", "cr_b"}, []string{"t_a"}, now)
	if withContradiction >= confidence {
		t.Fatalf("contradiction did not lower confidence: %.3f >= %.3f", withContradiction, confidence)
	}
	if status := CandidateStatus(withContradiction, 3, 1); status != "candidate" {
		t.Fatalf("contradicting status = %s, want candidate", status)
	}
}

func TestConditionsHashOrderIndependent(t *testing.T) {
	left := []Condition{{Field: "x", Operator: "=", Value: "a"}, {Field: "y", Operator: "=", Value: "b"}}
	right := []Condition{{Field: "y", Operator: "=", Value: "b"}, {Field: "x", Operator: "=", Value: "a"}}
	leftHash, _, err := ConditionsHash(left)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, _, err := ConditionsHash(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash != rightHash {
		t.Fatalf("hashes differ: %s != %s", leftHash, rightHash)
	}
}
