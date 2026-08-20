package utils

import (
	"crypto/rand"
	"fmt"
)

// CreateAgentId generates an agent identifier.
// Format: a{label-}{16 hex chars}
// Examples: a3f2c1b4d5e6f7a8, acompact-a3f2c1b4d5e6f7a8
// If label is empty, produces just a<16-hex-chars>.
// If label is non-empty, produces a<label>-<16-hex-chars>.
func CreateAgentId(label string) string {
	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	if err != nil {
		panic(fmt.Sprintf("failed to read random bytes: %v", err))
	}
	suffix := fmt.Sprintf("%x", buf)
	if label == "" {
		return "a" + suffix
	}
	return "a" + label + "-" + suffix
}
