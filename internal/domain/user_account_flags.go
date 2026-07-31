package domain

// UserAccountFlags holds per-user account annotations stored outside the base
// users row (admin fake badge, profile tab preference, etc.).
type UserAccountFlags struct {
	Fake           bool
	MainProfileTab string
}
