package utils

import (
	"crypto/rand"
	"encoding/hex"
)

func NewRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return EncodeID(int(bytes[0]) + 1)
	}
	return hex.EncodeToString(bytes[:])
}
