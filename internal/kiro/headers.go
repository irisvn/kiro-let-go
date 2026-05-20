package kiro

import "github.com/google/uuid"

// Headers is a bootstrap placeholder.
type Headers struct{}

// NewInvocationID returns a fresh UUID string.
func NewInvocationID() string { return uuid.NewString() }
