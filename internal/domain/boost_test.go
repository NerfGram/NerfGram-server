package domain

import (
	"errors"
	"testing"
)

func TestPremiumBoostStatusDefaultPolicyIsBoundedLinear(t *testing.T) {
	peer := Peer{Type: PeerTypeChannel, ID: 100}

	empty := PremiumBoostStatusForCount(peer, 0, nil)
	if empty.Level != MaxDefaultPremiumBoostLevel || empty.CurrentLevelBoosts != MaxDefaultPremiumBoostLevel || empty.Boosts != DefaultChannelBoostCount || empty.HasNextLevelBoosts {
		t.Fatalf("empty status = %+v, want level %d boosts %d", empty, MaxDefaultPremiumBoostLevel, DefaultChannelBoostCount)
	}

	one := PremiumBoostStatusForCount(peer, 1, nil)
	if one.Level != MaxDefaultPremiumBoostLevel || one.CurrentLevelBoosts != MaxDefaultPremiumBoostLevel || one.Boosts != DefaultChannelBoostCount+1 || one.HasNextLevelBoosts {
		t.Fatalf("one boost status = %+v, want capped max level with %d boosts", one, DefaultChannelBoostCount+1)
	}

	max := PremiumBoostStatusForCount(peer, MaxDefaultPremiumBoostLevel, nil)
	if max.Level != MaxDefaultPremiumBoostLevel || max.CurrentLevelBoosts != MaxDefaultPremiumBoostLevel || max.Boosts != DefaultChannelBoostCount+MaxDefaultPremiumBoostLevel || max.HasNextLevelBoosts {
		t.Fatalf("max status = %+v, want capped max level without next", max)
	}

	over := PremiumBoostStatusForCount(peer, MaxDefaultPremiumBoostLevel+50, nil)
	if over.Level != MaxDefaultPremiumBoostLevel || over.CurrentLevelBoosts != MaxDefaultPremiumBoostLevel || over.HasNextLevelBoosts {
		t.Fatalf("over max status = %+v, want capped max level without next", over)
	}
}

func TestPremiumBoostStatusCustomPolicy(t *testing.T) {
	policy := NewPremiumBoostLevelPolicy([]int{6, 1, 3, 3, 0})
	peer := Peer{Type: PeerTypeChannel, ID: 100}

	status := PremiumBoostStatusForPolicy(peer, 4, nil, policy)
	if status.Level != 2 || status.CurrentLevelBoosts != 3 || status.NextLevelBoosts != 6 || !status.HasNextLevelBoosts {
		t.Fatalf("custom policy status = %+v, want level 2 current 3 next 6", status)
	}

	status = PremiumBoostStatusForPolicy(peer, 6, nil, policy)
	if status.Level != 3 || status.CurrentLevelBoosts != 6 || status.HasNextLevelBoosts {
		t.Fatalf("custom max status = %+v, want level 3 without next", status)
	}
}

func TestApplyPremiumBoostSlotLifecycle(t *testing.T) {
	peer := Peer{Type: PeerTypeChannel, ID: 100}
	slot, changed, err := ApplyPremiumBoostSlot(PremiumBoostSlot{}, 10, DefaultPremiumBoostSlotID, peer, 1000, 2000, 0)
	if err != nil || !changed {
		t.Fatalf("apply free slot changed=%v err=%v", changed, err)
	}
	if slot.UserID != 10 || slot.Slot != DefaultPremiumBoostSlotID || slot.Peer != peer || slot.Date != 1000 || slot.Expires != 2000 || slot.Multiplier != 1 {
		t.Fatalf("applied slot = %+v", slot)
	}

	_, changed, err = ApplyPremiumBoostSlot(slot, 10, DefaultPremiumBoostSlotID, peer, 1001, 2000, 0)
	if !errors.Is(err, ErrBoostNotModified) || changed {
		t.Fatalf("reapply same peer changed=%v err=%v, want ErrBoostNotModified", changed, err)
	}

	extended, changed, err := ApplyPremiumBoostSlot(slot, 10, DefaultPremiumBoostSlotID, peer, 1002, 3000, 0)
	if err != nil || !changed || extended.Expires != 3000 || extended.Date != slot.Date {
		t.Fatalf("extend same peer slot=%+v changed=%v err=%v", extended, changed, err)
	}

	shortened, changed, err := ApplyPremiumBoostSlot(extended, 10, DefaultPremiumBoostSlotID, peer, 1003, 2500, 0)
	if err != nil || !changed || shortened.Expires != 2500 || shortened.Date != slot.Date {
		t.Fatalf("refresh same peer expiry slot=%+v changed=%v err=%v", shortened, changed, err)
	}
}

func TestApplyPremiumBoostSlotCooldown(t *testing.T) {
	first := Peer{Type: PeerTypeChannel, ID: 100}
	second := Peer{Type: PeerTypeChannel, ID: 200}
	slot := PremiumBoostSlot{
		UserID:        10,
		Slot:          DefaultPremiumBoostSlotID,
		Peer:          first,
		Date:          1000,
		Expires:       3000,
		CooldownUntil: 1500,
		Multiplier:    1,
	}

	_, changed, err := ApplyPremiumBoostSlot(slot, 10, DefaultPremiumBoostSlotID, second, 1200, 3000, 0)
	if changed || err == nil {
		t.Fatalf("cooldown apply changed=%v err=%v, want flood wait", changed, err)
	}
	if seconds, ok := PremiumBoostFloodWaitSeconds(err); !ok || seconds != 300 {
		t.Fatalf("cooldown err = %v seconds=%d ok=%v, want 300", err, seconds, ok)
	}
}
