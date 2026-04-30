package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

func SHA256Key(parts ...string) string {
	joined := strings.Join(parts, "\x1f")
	sum := sha256.Sum256([]byte(joined))
	return hex.EncodeToString(sum[:])
}

func JSONHash(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func CanonicalizeConditions(conditions []Condition) []Condition {
	canonical := append([]Condition(nil), conditions...)
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].Field != canonical[j].Field {
			return canonical[i].Field < canonical[j].Field
		}
		if canonical[i].Operator != canonical[j].Operator {
			return canonical[i].Operator < canonical[j].Operator
		}
		left, _ := json.Marshal(canonical[i].Value)
		right, _ := json.Marshal(canonical[j].Value)
		return string(left) < string(right)
	})
	return canonical
}

func ConditionsHash(conditions []Condition) (string, []Condition, error) {
	canonical := CanonicalizeConditions(conditions)
	hash, err := JSONHash(canonical)
	return hash, canonical, err
}
