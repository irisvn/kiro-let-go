package kiro

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const machineIDSalt = "KiroIDE-MachineID-v1"

// Generate returns a deterministic machine fingerprint for the seed.
func Generate(seed string) string {
	sum := sha256.Sum256([]byte(seed + machineIDSalt))
	return hex.EncodeToString(sum[:])
}

// Validate checks that the machine ID is a lowercase hex SHA256 string.
func Validate(id string) error {
	if len(id) != 64 {
		return fmt.Errorf("invalid machine id length: %d", len(id))
	}
	for _, r := range id {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return fmt.Errorf("invalid machine id character: %q", r)
		}
	}
	return nil
}
