package domain

// LoginCodeDeliveryRequest describes one durable 777000 login-code delivery.
// PhoneCodeHash is an opaque idempotency token and must never be persisted in
// plaintext; store implementations persist only its SHA-256 digest.
type LoginCodeDeliveryRequest struct {
	UserID        int64
	PhoneCodeHash string
	Code          string
	Date          int
	// ExpiresAt is the unix second after which the compact idempotency receipt
	// may be reclaimed. It must cover the corresponding code's usable lifetime.
	ExpiresAt int64
}

// LoginCodeDeliveryResult returns the immutable first delivery. Created is
// false when the same phone_code_hash was already committed and replayed.
type LoginCodeDeliveryResult struct {
	Message Message
	Created bool
}
