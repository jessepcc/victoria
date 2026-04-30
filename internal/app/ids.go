package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

type IDGenerator interface {
	NewID(prefix string) string
}

type RandomIDs struct{}

func (RandomIDs) NewID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
