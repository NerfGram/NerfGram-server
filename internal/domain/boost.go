package domain

import (
	"errors"
	"fmt"
	"sort"
)

const (
	// DefaultChannelBoostCount is the baseline boost count every channel starts
	// with. These boosts never expire (not stored in channel_boost_slots).
	DefaultChannelBoostCount = 100
	// DefaultPremiumBoostSlotID is the single base boost slot every active premium
	// user owns in the current small-scale implementation.
	DefaultPremiumBoostSlotID = 1
	// MaxPremiumBoostSlotsPerApply bounds premium.applyBoost slots before storage.
	MaxPremiumBoostSlotsPerApply = 16
	// MaxPremiumBoostsListLimit bounds premium.getBoostsList pages.
	MaxPremiumBoostsListLimit = 100
	// MaxPremiumBoostsOffsetBytes bounds opaque boost pagination cursors.
	MaxPremiumBoostsOffsetBytes = 128
	// MaxChannelBoostsToUnblockRestrictions matches Layer 225 TDesktop UI bounds.
	MaxChannelBoostsToUnblockRestrictions = 8
	// MaxDefaultPremiumBoostLevel bounds the built-in level table so status replies
	// never expose unbounded next-level values to clients.
	MaxDefaultPremiumBoostLevel = 100
	// DefaultPremiumBoostReassignCooldownSeconds keeps the current dev behavior of
	// allowing immediate moves while preserving a single hook for official cooldowns.
	DefaultPremiumBoostReassignCooldownSeconds = 0
)

var (
	ErrBoostNotModified = errors.New("boost not modified")

	defaultPremiumBoostLevelPolicy = PremiumBoostLevelPolicy{
		Thresholds: linearPremiumBoostThresholds(MaxDefaultPremiumBoostLevel),
	}
)

// PremiumBoostSlot describes one user-owned boost slot without TL types.
type PremiumBoostSlot struct {
	UserID        int64
	Slot          int
	Peer          Peer
	Date          int
	Expires       int
	CooldownUntil int
	Multiplier    int
	Gift          bool
	Giveaway      bool
	Unclaimed     bool
	GiveawayMsgID int
	UsedGiftSlug  string
	Stars         int64
}

// Active reports whether the slot can currently count for a peer.
func (s PremiumBoostSlot) Active(now int) bool {
	if s.UserID == 0 || s.Slot <= 0 {
		return false
	}
	return s.Expires == 0 || s.Expires > now
}

// Assigned reports whether the active slot is currently applied to a peer.
func (s PremiumBoostSlot) Assigned(now int) bool {
	return s.Active(now) && s.Peer.ID != 0
}

// Weight returns the boost contribution of one active assigned slot.
func (s PremiumBoostSlot) Weight(now int) int {
	if !s.Assigned(now) {
		return 0
	}
	if s.Multiplier <= 0 {
		return 1
	}
	return s.Multiplier
}

// PremiumBoostStatus is the domain projection behind premium.boostsStatus.
type PremiumBoostStatus struct {
	Peer                 Peer
	Level                int
	CurrentLevelBoosts   int
	Boosts               int
	GiftBoosts           int
	NextLevelBoosts      int
	HasNextLevelBoosts   bool
	PremiumAudiencePart  int
	PremiumAudienceTotal int
	MyBoostSlots         []PremiumBoostSlot
}

// PremiumBoostList is a bounded admin-visible boosts page.
type PremiumBoostList struct {
	Count      int
	Boosts     []PremiumBoostSlot
	Users      []User
	NextOffset string
}

// PremiumMyBoosts is the current user's slot inventory.
type PremiumMyBoosts struct {
	Slots    []PremiumBoostSlot
	Users    []User
	Channels []Channel
}

// PremiumBoostLevelPolicy maps cumulative active boosts to channel boost levels.
// Thresholds[n-1] is the minimum boost count required for level n.
type PremiumBoostLevelPolicy struct {
	Thresholds []int
}

// DefaultPremiumBoostLevelPolicy returns the current bounded telesrv level table.
// It is intentionally policy-backed: production deployments can replace the table
// with official thresholds without touching store/RPC state transitions.
func DefaultPremiumBoostLevelPolicy() PremiumBoostLevelPolicy {
	return PremiumBoostLevelPolicy{
		Thresholds: append([]int(nil), defaultPremiumBoostLevelPolicy.Thresholds...),
	}
}

// NewPremiumBoostLevelPolicy creates a monotonic threshold policy from raw values.
func NewPremiumBoostLevelPolicy(thresholds []int) PremiumBoostLevelPolicy {
	return PremiumBoostLevelPolicy{Thresholds: normalizePremiumBoostThresholds(thresholds)}
}

// LevelForBoosts returns level bounds for the given active boost count.
func (p PremiumBoostLevelPolicy) LevelForBoosts(boosts int) (level, currentLevelBoosts, nextLevelBoosts int, hasNext bool) {
	if boosts < 0 {
		boosts = 0
	}
	thresholds := p.thresholds()
	if len(thresholds) == 0 {
		return 0, 0, 0, false
	}
	level = sort.Search(len(thresholds), func(i int) bool { return thresholds[i] > boosts })
	if level > 0 {
		currentLevelBoosts = thresholds[level-1]
	}
	if level < len(thresholds) {
		return level, currentLevelBoosts, thresholds[level], true
	}
	return len(thresholds), thresholds[len(thresholds)-1], 0, false
}

func (p PremiumBoostLevelPolicy) thresholds() []int {
	if len(p.Thresholds) == 0 {
		return defaultPremiumBoostLevelPolicy.Thresholds
	}
	last := 0
	for _, threshold := range p.Thresholds {
		if threshold <= 0 || threshold <= last {
			return normalizePremiumBoostThresholds(p.Thresholds)
		}
		last = threshold
	}
	return p.Thresholds
}

func normalizePremiumBoostThresholds(thresholds []int) []int {
	if len(thresholds) == 0 {
		return nil
	}
	sorted := make([]int, 0, len(thresholds))
	for _, threshold := range thresholds {
		if threshold > 0 {
			sorted = append(sorted, threshold)
		}
	}
	sort.Ints(sorted)
	out := sorted[:0]
	last := 0
	for _, threshold := range sorted {
		if threshold == last {
			continue
		}
		out = append(out, threshold)
		last = threshold
	}
	return append([]int(nil), out...)
}

func linearPremiumBoostThresholds(maxLevel int) []int {
	if maxLevel <= 0 {
		return nil
	}
	out := make([]int, maxLevel)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

// PremiumBoostStatusForCount returns the bounded default level projection.
func PremiumBoostStatusForCount(peer Peer, boosts int, my []PremiumBoostSlot) PremiumBoostStatus {
	boosts += DefaultChannelBoostCount
	return PremiumBoostStatusForPolicy(peer, boosts, my, defaultPremiumBoostLevelPolicy)
}

// PremiumBoostStatusForPolicy returns a status projection from an explicit level policy.
func PremiumBoostStatusForPolicy(peer Peer, boosts int, my []PremiumBoostSlot, policy PremiumBoostLevelPolicy) PremiumBoostStatus {
	if boosts < 0 {
		boosts = 0
	}
	level, current, next, hasNext := policy.LevelForBoosts(boosts)
	return PremiumBoostStatus{
		Peer:               peer,
		Level:              level,
		CurrentLevelBoosts: current,
		Boosts:             boosts,
		NextLevelBoosts:    next,
		HasNextLevelBoosts: hasNext,
		MyBoostSlots:       append([]PremiumBoostSlot(nil), my...),
	}
}

// ApplyPremiumBoostSlot applies one user-owned slot to a peer and enforces shared
// lifecycle rules for all store implementations.
func ApplyPremiumBoostSlot(slot PremiumBoostSlot, userID int64, slotID int, peer Peer, now, premiumUntil, cooldownSeconds int) (PremiumBoostSlot, bool, error) {
	if userID == 0 || slotID <= 0 || peer.Type != PeerTypeChannel || peer.ID == 0 || now < 0 {
		return PremiumBoostSlot{}, false, ErrChannelInvalid
	}
	if premiumUntil <= now {
		return PremiumBoostSlot{}, false, ErrPremiumRequired
	}
	if cooldownSeconds < 0 {
		cooldownSeconds = 0
	}
	if slot.UserID == 0 || slot.Slot == 0 || !slot.Active(now) {
		slot = PremiumBoostSlot{
			UserID:     userID,
			Slot:       slotID,
			Multiplier: 1,
		}
	}
	if slot.UserID != userID || slot.Slot != slotID {
		return PremiumBoostSlot{}, false, ErrChannelInvalid
	}
	if slot.Multiplier <= 0 {
		slot.Multiplier = 1
	}
	if slot.Assigned(now) && slot.Peer == peer {
		if slot.Expires != 0 && slot.Expires != premiumUntil {
			slot.Expires = premiumUntil
			return slot, true, nil
		}
		return slot, false, ErrBoostNotModified
	}
	if slot.Assigned(now) && slot.Peer != peer && slot.CooldownUntil > now {
		return slot, false, NewPremiumBoostFloodWaitError(slot.CooldownUntil - now)
	}
	wasAssigned := slot.Assigned(now)
	slot.Peer = peer
	slot.Date = now
	slot.Expires = premiumUntil
	if wasAssigned && cooldownSeconds > 0 {
		slot.CooldownUntil = now + cooldownSeconds
	} else if slot.CooldownUntil <= now {
		slot.CooldownUntil = 0
	}
	return slot, true, nil
}

// PremiumBoostFloodWaitError carries remaining seconds for boost reassign cooldowns.
type PremiumBoostFloodWaitError struct {
	Seconds int
}

func (e PremiumBoostFloodWaitError) Error() string {
	if e.Seconds <= 0 {
		return "premium boost flood wait"
	}
	return fmt.Sprintf("premium boost flood wait %d seconds", e.Seconds)
}

func NewPremiumBoostFloodWaitError(seconds int) error {
	if seconds <= 0 {
		seconds = 1
	}
	return PremiumBoostFloodWaitError{Seconds: seconds}
}

func PremiumBoostFloodWaitSeconds(err error) (int, bool) {
	var wait PremiumBoostFloodWaitError
	if !errors.As(err, &wait) {
		return 0, false
	}
	if wait.Seconds <= 0 {
		return 1, true
	}
	return wait.Seconds, true
}
