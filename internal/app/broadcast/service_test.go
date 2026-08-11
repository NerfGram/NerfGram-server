package broadcast

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"telesrv/internal/domain"
	"telesrv/internal/store"
)

func TestNormalizeRequestRejectsAmbiguousTargets(t *testing.T) {
	tests := []struct {
		name    string
		message string
		mode    domain.BroadcastTargetMode
		ids     []int64
		wantErr error
	}{
		{name: "empty", message: "  ", mode: domain.BroadcastTargetAll, wantErr: domain.ErrBroadcastMessageEmpty},
		{name: "too long", message: strings.Repeat("x", domain.MaxBroadcastMessageBytes+1), mode: domain.BroadcastTargetAll, wantErr: domain.ErrBroadcastMessageTooLong},
		{name: "all with ids", message: "hello", mode: domain.BroadcastTargetAll, ids: []int64{1}, wantErr: domain.ErrBroadcastInvalid},
		{name: "selected empty", message: "hello", mode: domain.BroadcastTargetSelected, wantErr: domain.ErrBroadcastNoRecipients},
		{name: "selected duplicate", message: "hello", mode: domain.BroadcastTargetSelected, ids: []int64{1, 1}, wantErr: domain.ErrBroadcastRecipientInvalid},
		{name: "selected system", message: "hello", mode: domain.BroadcastTargetSelected, ids: []int64{domain.OfficialSystemUserID}, wantErr: domain.ErrBroadcastRecipientInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := normalizeRequest(tt.message, tt.mode, tt.ids)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizeRequest error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRunCycleReleasesOnlyFailedClaims(t *testing.T) {
	claims := []store.BroadcastRecipientClaim{
		{RecipientID: 1, BroadcastID: 10, UserID: 101, LeaseToken: "lease", Message: "hello"},
		{RecipientID: 2, BroadcastID: 10, UserID: 102, LeaseToken: "lease", Message: "hello"},
	}
	st := &fakeBroadcastStore{materialized: 3, claims: claims}
	delivery := &fakeBroadcastDelivery{failRecipientID: 2}
	service := NewService(st, delivery, nil)

	result, err := service.RunCycle(context.Background(), "lease", 20, 10, 30*time.Second)
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}
	if result != (CycleResult{Materialized: 3, Claimed: 2, Sent: 1, Failed: 1}) {
		t.Fatalf("RunCycle result = %+v", result)
	}
	if len(st.released) != 1 || st.released[0].RecipientID != 2 {
		t.Fatalf("released claims = %+v, want recipient 2 only", st.released)
	}
	if delivery.calls != 2 {
		t.Fatalf("delivery calls = %d, want 2", delivery.calls)
	}
}

type fakeBroadcastStore struct {
	materialized int
	claims       []store.BroadcastRecipientClaim
	released     []store.BroadcastRecipientClaim
}

func (s *fakeBroadcastStore) PreviewBroadcastRecipients(context.Context, domain.BroadcastTargetMode, []int64) (int64, error) {
	return 0, nil
}

func (s *fakeBroadcastStore) CreateBroadcast(context.Context, string, domain.BroadcastTargetMode, []int64, string) (domain.Broadcast, error) {
	return domain.Broadcast{}, nil
}

func (s *fakeBroadcastStore) MaterializeBroadcastRecipients(context.Context, int) (int, error) {
	return s.materialized, nil
}

func (s *fakeBroadcastStore) ClaimBroadcastRecipients(context.Context, string, int, time.Duration) ([]store.BroadcastRecipientClaim, error) {
	return s.claims, nil
}

func (s *fakeBroadcastStore) ReleaseBroadcastRecipient(_ context.Context, claim store.BroadcastRecipientClaim, _ string) error {
	s.released = append(s.released, claim)
	return nil
}

func (s *fakeBroadcastStore) ListBroadcasts(context.Context, int64, int) ([]domain.Broadcast, bool, error) {
	return nil, false, nil
}

type fakeBroadcastDelivery struct {
	failRecipientID int64
	calls           int
}

func (d *fakeBroadcastDelivery) DeliverBroadcastRecipient(_ context.Context, claim store.BroadcastRecipientClaim) (domain.Message, error) {
	d.calls++
	if claim.RecipientID == d.failRecipientID {
		return domain.Message{}, errors.New("synthetic delivery failure")
	}
	return domain.Message{OwnerUserID: claim.UserID}, nil
}
