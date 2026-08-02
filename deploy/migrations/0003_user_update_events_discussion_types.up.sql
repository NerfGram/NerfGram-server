-- 修复：forum 话题已读（0002/a180fbc）新增了 user_update_events 事件类型
-- read_channel_discussion_inbox / read_channel_discussion_outbox（updateReadChannelDiscussionInbox/
-- Outbox 的 durable 多设备同步），但 0001 的 user_update_events_type_check 白名单未包含它们。
-- 后果：真 PostgreSQL 上 messages.readDiscussion 首次标已读时 append durable 事件被 CHECK
-- 约束拒绝（SQLSTATE 23514）→ RPC 返回 500，且话题已读无法进入 durable / getDifference，
-- 用户其它设备收不到 per-topic 已读。memory store 无 CHECK 约束故单测未暴露，真双机才发现。
ALTER TABLE public.user_update_events DROP CONSTRAINT IF EXISTS user_update_events_type_check;
ALTER TABLE public.user_update_events ADD CONSTRAINT user_update_events_type_check CHECK (
    (event_type)::text = ANY (ARRAY[
        'new_message', 'read_history_inbox', 'read_history_outbox', 'read_message_contents',
        'edit_message', 'message_reactions', 'message_poll', 'draft_message', 'quick_replies',
        'new_quick_reply', 'delete_quick_reply', 'quick_reply_message', 'delete_quick_reply_messages',
        'contacts_reset', 'dialog_pinned', 'pinned_dialogs', 'pinned_messages', 'dialog_unread_mark',
        'peer_settings', 'peer_story_blocked', 'delete_messages', 'dialog_filter', 'dialog_filter_order',
        'dialog_filters', 'folder_peers', 'channel_view_forum_as_messages',
        'channel_state', 'saved_dialog_pinned', 'pinned_saved_dialogs', 'story', 'read_stories',
        'sent_story_reaction', 'new_story_reaction', 'noop',
        'read_channel_discussion_inbox', 'read_channel_discussion_outbox'
    ]::text[])
);
