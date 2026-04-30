package domain

import (
	"math"
	"time"
)

const (
	DefaultUnderReviewThreshold = 0.55
	DefaultMinEvidenceCount     = 3
	wilsonZ                     = 1.28
	recencyHalfLifeDays         = 30.0
	recencyFloor                = 0.1
)

func WilsonLower(evidenceCount, totalCount int) float64 {
	if evidenceCount <= 0 || totalCount <= 0 {
		return 0
	}
	e := float64(evidenceCount)
	n := float64(totalCount)
	pHat := e / n
	z2 := wilsonZ * wilsonZ
	denominator := 1 + z2/n
	centre := pHat + z2/(2*n)
	margin := wilsonZ * math.Sqrt((pHat*(1-pHat)/n)+(z2/(4*n*n)))
	return clamp01((centre - margin) / denominator)
}

func RecencyFactor(now, lastSeenAt time.Time) float64 {
	if lastSeenAt.IsZero() || now.Before(lastSeenAt) {
		return 1
	}
	ageDays := now.Sub(lastSeenAt).Hours() / 24
	return math.Max(recencyFloor, math.Exp(-ageDays/recencyHalfLifeDays))
}

func ScopeConsistencyFactor(sourceCaseRunIDs []string, sourceTenantIDs []string) float64 {
	distinctCases := len(distinct(sourceCaseRunIDs))
	distinctTenants := len(distinct(sourceTenantIDs))
	if distinctCases == 0 {
		distinctCases = 1
	}
	if distinctTenants == 0 {
		distinctTenants = 1
	}
	score := 0.8 + 0.1*float64(distinctCases-1) + 0.05*float64(distinctTenants-1)
	return math.Min(1, score)
}

func CandidateConfidence(now time.Time, evidenceCount, contradictingCount int, sourceCaseRunIDs []string, sourceTenantIDs []string, lastSeenAt time.Time) float64 {
	total := evidenceCount + contradictingCount
	return clamp01(WilsonLower(evidenceCount, total) *
		RecencyFactor(now, lastSeenAt) *
		ScopeConsistencyFactor(sourceCaseRunIDs, sourceTenantIDs))
}

func CandidateStatus(confidence float64, evidenceCount, contradictingCount int) string {
	if confidence >= DefaultUnderReviewThreshold && evidenceCount >= DefaultMinEvidenceCount && contradictingCount == 0 {
		return "under_review"
	}
	return "candidate"
}

func distinct(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
