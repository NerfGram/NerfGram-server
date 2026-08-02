package postgres

import (
	"context"
	"testing"

	"telesrv/internal/domain"
)

// TestChannelStoreEnablingDirectMessagesCreatesMonoforum 回归迁移 0020 + monoforum 生命周期:
// 开启频道私信(Direct Messages)为广播母频道事务内建/关联一个 monoforum 虚拟频道,
// 新列(monoforum/linked_monoforum_id)经共享 channelColumns + scan 正确往返;再次开启复用、
// 同步价格;非广播不建。门控于 TELESRV_TEST_POSTGRES_DSN。
func TestChannelStoreEnablingDirectMessagesCreatesMonoforum(t *testing.T) {
	pool := testPool(t) // 未设 DSN 会 t.Skip
	ctx := context.Background()
	suffix := randomSuffix(t)

	users := NewUserStore(pool)
	owner, err := users.Create(ctx, domain.User{AccessHash: 81, Phone: "+1779" + suffix + "31", FirstName: "MonoOwner"})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	var channelIDs []int64
	t.Cleanup(func() {
		if len(channelIDs) > 0 {
			_, _ = pool.Exec(ctx, "DELETE FROM channels WHERE id = ANY($1::bigint[])", channelIDs)
		}
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", owner.ID)
	})

	channels := NewChannelStore(pool)
	created, err := channels.CreateChannel(ctx, domain.CreateChannelRequest{
		CreatorUserID: owner.ID, Title: "Mono Broadcast " + suffix, Broadcast: true, Date: 1700000900,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	channelIDs = append(channelIDs, created.Channel.ID)

	updatedResult, err := channels.SetPaidMessagesPrice(ctx, owner.ID, created.Channel.ID, 5, true)
	if err != nil {
		t.Fatalf("enable DM: %v", err)
	}
	updated := updatedResult.Channel
	if updated.LinkedMonoforumID == 0 || !updated.BroadcastMessagesAllowed || updated.SendPaidMessagesStars != 5 {
		t.Fatalf("after enable = %+v, want linked monoforum + broadcast allowed + 5 stars", updated)
	}
	monoID := updated.LinkedMonoforumID
	channelIDs = append(channelIDs, monoID)

	// 母频道新列经 scan 读回(scanChannelWithMember 路径)。
	view, err := channels.GetChannel(ctx, owner.ID, created.Channel.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if view.Channel.LinkedMonoforumID != monoID || view.Channel.Monoforum {
		t.Fatalf("parent view = monoforum %v linked %d, want not-monoforum linked %d", view.Channel.Monoforum, view.Channel.LinkedMonoforumID, monoID)
	}

	// monoforum 行:monoforum=true、反向关联母频道、价格、creator。
	var mfMono, mfBroadcast, mfMegagroup bool
	var mfLinked, mfStars, mfCreator int64
	var mfTop, mfPts int
	if err := pool.QueryRow(ctx, `SELECT monoforum, broadcast, megagroup, linked_monoforum_id, send_paid_messages_stars, creator_user_id, top_message_id, pts FROM channels WHERE id = $1`, monoID).Scan(&mfMono, &mfBroadcast, &mfMegagroup, &mfLinked, &mfStars, &mfCreator, &mfTop, &mfPts); err != nil {
		t.Fatalf("read monoforum row: %v", err)
	}
	if !mfMono || mfBroadcast || !mfMegagroup || mfLinked != created.Channel.ID || mfStars != 5 || mfCreator != owner.ID {
		t.Fatalf("monoforum row = mono %v broadcast %v megagroup %v linked %d stars %d creator %d, want true/false/true/%d/5/%d", mfMono, mfBroadcast, mfMegagroup, mfLinked, mfStars, mfCreator, created.Channel.ID, owner.ID)
	}
	if mfTop == 0 || mfPts == 0 {
		t.Fatalf("monoforum top/pts = %d/%d, want paid-messages service top", mfTop, mfPts)
	}
	read, err := channels.ReadChannelHistory(ctx, domain.ReadChannelHistoryRequest{
		UserID: owner.ID, ChannelID: monoID, MaxID: mfTop, Date: 1700000901,
	})
	if err != nil {
		t.Fatalf("read synthetic monoforum history: %v", err)
	}
	if !read.ReadOnly || read.Changed || read.MaxID != mfTop {
		t.Fatalf("synthetic monoforum read = %+v, want read-only no-op at %d", read, mfTop)
	}
	var memberExists, dialogExists bool
	if err := pool.QueryRow(ctx, `
SELECT EXISTS (
           SELECT 1 FROM channel_members WHERE channel_id=$1 AND user_id=$2
       ),
       EXISTS (
           SELECT 1 FROM channel_dialogs WHERE channel_id=$1 AND user_id=$2
       )`, monoID, owner.ID).Scan(&memberExists, &dialogExists); err != nil {
		t.Fatalf("check synthetic monoforum read footprint: %v", err)
	}
	if memberExists || dialogExists {
		t.Fatalf("synthetic monoforum read persisted member/dialog = %v/%v", memberExists, dialogExists)
	}
	// 同批下发母广播频道(TDesktop 据此 resolve linked_monoforum_id 并派生 MonoforumAdmin
	// 渲染 Direct-Messages 容器):GetChannelDialogs([mono]) 的 chats[] 必须同时带 mono 与母频道。
	coDelivery, err := channels.GetChannelDialogs(ctx, owner.ID, []int64{monoID})
	if err != nil {
		t.Fatalf("get channel dialogs: %v", err)
	}
	var hasMono, hasParent bool
	for _, ch := range coDelivery.Channels {
		switch ch.ID {
		case monoID:
			hasMono = true
		case created.Channel.ID:
			hasParent = true
		}
	}
	if !hasMono || !hasParent {
		t.Fatalf("GetChannelDialogs([mono]) hasMono=%v hasParent=%v, want both (parent co-delivered): %+v", hasMono, hasParent, coDelivery.Channels)
	}
	dialogs, err := channels.ListChannelDialogs(ctx, owner.ID, domain.DialogFilter{Limit: 100})
	if err != nil {
		t.Fatalf("list dialogs after enable: %v", err)
	}
	var foundMono bool
	for _, dialog := range dialogs.Dialogs {
		if dialog.Peer == (domain.Peer{Type: domain.PeerTypeChannel, ID: monoID}) {
			foundMono = true
			if dialog.TopMessage != mfTop || dialog.UnreadCount != 0 || dialog.ReadInboxMaxID < mfTop {
				t.Fatalf("monoforum dialog = %+v, want top %d read/no-unread", dialog, mfTop)
			}
		}
	}
	if !foundMono {
		t.Fatalf("dialogs after enable do not include admin-visible monoforum %d: %+v", monoID, dialogs.Dialogs)
	}
	// monoforum 只有一条创建服务消息(TDesktop 渲染 "Direct messages were enabled in this channel.",
	// lng_action_created_monoforum)。开关/价格变更的 paid_messages_price 只进母广播频道(在 broadcast
	// 历史里渲染 "Channel enabled/disabled Direct Messages"),绝不进 mono——否则 mono 是 megagroup,
	// 同一 action 会渲染成错误的"消息免费/设价"文案。
	var actionType string
	var monoMsgCount int
	assertMonoSoleCreation := func(label string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM channel_messages WHERE channel_id = $1`, monoID).Scan(&monoMsgCount); err != nil {
			t.Fatalf("count monoforum messages (%s): %v", label, err)
		}
		if monoMsgCount != 1 {
			t.Fatalf("monoforum messages (%s) = %d, want exactly one creation service", label, monoMsgCount)
		}
		if err := pool.QueryRow(ctx, `SELECT action->>'Type' FROM channel_messages WHERE channel_id = $1 ORDER BY id DESC LIMIT 1`, monoID).Scan(&actionType); err != nil {
			t.Fatalf("read monoforum service action (%s): %v", label, err)
		}
		if actionType != string(domain.ChannelActionCreate) {
			t.Fatalf("monoforum service action (%s) = %q, want channel_create", label, actionType)
		}
	}
	assertMonoSoleCreation("after enable")

	var parentActionType string
	var parentActionAllowed bool
	var parentActionStars int64
	readParentAction := func() {
		t.Helper()
		if err := pool.QueryRow(ctx, `
SELECT action->>'Type', (action->>'BroadcastMessagesAllowed')::boolean, (action->>'Stars')::bigint
FROM channel_messages
WHERE channel_id = $1
ORDER BY id DESC
LIMIT 1`, created.Channel.ID).Scan(&parentActionType, &parentActionAllowed, &parentActionStars); err != nil {
			t.Fatalf("read parent paid messages service action: %v", err)
		}
	}
	readParentAction()
	if parentActionType != string(domain.ChannelActionPaidMessagesPrice) || !parentActionAllowed || parentActionStars != 5 {
		t.Fatalf("parent service action = %q/%v/%d, want paid_messages_price/true/5", parentActionType, parentActionAllowed, parentActionStars)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM channel_messages WHERE channel_id = $1`, monoID); err != nil {
		t.Fatalf("delete monoforum service message for legacy simulation: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM channel_update_events WHERE channel_id = $1`, monoID); err != nil {
		t.Fatalf("delete monoforum service event for legacy simulation: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE channels SET top_message_id = 0, pts = 0 WHERE id = $1`, monoID); err != nil {
		t.Fatalf("reset monoforum top for legacy simulation: %v", err)
	}
	repairedResult, err := channels.SetPaidMessagesPrice(ctx, owner.ID, created.Channel.ID, 5, true)
	if err != nil {
		t.Fatalf("repair legacy DM: %v", err)
	}
	repaired := repairedResult.Channel
	if repaired.LinkedMonoforumID != monoID {
		t.Fatalf("repair linked = %d, want stable %d", repaired.LinkedMonoforumID, monoID)
	}
	// 修复后 monoforum 重新获得一条创建消息。
	assertMonoSoleCreation("after legacy repair")

	// 幂等 + 价格同步:再次开启不新建 monoforum,价格改 8;变更只进母频道。
	againResult, err := channels.SetPaidMessagesPrice(ctx, owner.ID, created.Channel.ID, 8, true)
	if err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	again := againResult.Channel
	if again.LinkedMonoforumID != monoID {
		t.Fatalf("re-enable linked = %d, want stable %d (no second monoforum)", again.LinkedMonoforumID, monoID)
	}
	if err := pool.QueryRow(ctx, `SELECT send_paid_messages_stars FROM channels WHERE id = $1`, monoID).Scan(&mfStars); err != nil {
		t.Fatalf("read monoforum price: %v", err)
	}
	if mfStars != 8 {
		t.Fatalf("monoforum price = %d, want synced 8", mfStars)
	}
	assertMonoSoleCreation("after price update")
	readParentAction()
	if parentActionType != string(domain.ChannelActionPaidMessagesPrice) || !parentActionAllowed || parentActionStars != 8 {
		t.Fatalf("parent latest action = %q/%v/%d, want paid_messages_price/true/8", parentActionType, parentActionAllowed, parentActionStars)
	}
	disabledResult, err := channels.SetPaidMessagesPrice(ctx, owner.ID, created.Channel.ID, 0, false)
	if err != nil {
		t.Fatalf("disable DM: %v", err)
	}
	disabled := disabledResult.Channel
	if disabled.LinkedMonoforumID != monoID || disabled.BroadcastMessagesAllowed {
		t.Fatalf("disable linked/allowed = %d/%v, want stable %d and disabled", disabled.LinkedMonoforumID, disabled.BroadcastMessagesAllowed, monoID)
	}
	if disabledResult.ServiceMessage == nil || disabledResult.ServiceMessage.Event.Pts == 0 {
		t.Fatalf("disable service result = %+v, want paid_messages_price event", disabledResult.ServiceMessage)
	}
	// 关闭只进母频道(+monoforumDisabled 状态);monoforum 仍只有创建消息那一条。
	assertMonoSoleCreation("after disable")
	readParentAction()
	if parentActionType != string(domain.ChannelActionPaidMessagesPrice) || parentActionAllowed || parentActionStars != 0 {
		t.Fatalf("parent latest action after disable = %q/%v/%d, want paid_messages_price/false/0", parentActionType, parentActionAllowed, parentActionStars)
	}

	// 超级群(非广播)开 paid messages 不建 monoforum。
	group, err := channels.CreateChannel(ctx, domain.CreateChannelRequest{
		CreatorUserID: owner.ID, Title: "Mono SG " + suffix, Megagroup: true, Date: 1700000902,
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	channelIDs = append(channelIDs, group.Channel.ID)
	sgResult, err := channels.SetPaidMessagesPrice(ctx, owner.ID, group.Channel.ID, 3, true)
	if err != nil {
		t.Fatalf("group paid: %v", err)
	}
	sg := sgResult.Channel
	if sg.LinkedMonoforumID != 0 || sg.BroadcastMessagesAllowed {
		t.Fatalf("megagroup = linked %d broadcastAllowed %v, want no monoforum", sg.LinkedMonoforumID, sg.BroadcastMessagesAllowed)
	}
}
