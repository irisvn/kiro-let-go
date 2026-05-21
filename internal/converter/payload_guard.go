package converter

import "github.com/irisvn/kiro-let-go/internal/kiro"

const MaxPayloadBytes = kiro.MaxPayloadBytes

func GuardPayloadSize(payload *kiro.KiroPayload) error {
	return kiro.GuardPayloadSize(payload)
}
