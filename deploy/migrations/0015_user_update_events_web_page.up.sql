-- 链接预览异步回填（updateWebPage）新增 user_update_events 事件类型 'web_page'：发送时挂的
-- pending 链接预览占位被带外解析后，经 EditMessage 的 WebPageResolve 模式就地替换并 append
-- 一条 web_page durable 事件（按 webPage id 关联，不标记「已编辑」）。0001/0003 的白名单未含它，
-- 真 Postgres 上 append 会被 CHECK 约束拒绝（SQLSTATE 23514）；memory store 无约束故单测未暴露。
ALTER TABLE public.user_update_events DROP CONSTRAINT IF EXISTS user_update_events_type_check;
ALTER TABLE public.user_update_events ADD CONSTRAINT user_update_events_type_check CHECK (
    (event_type)::text = ANY (ARRAY[
        'new_message', 'read_history_inbox', 'read_history_outbox', 'read_message_contents',
        'edit_message', 'web_page', 'message_reactions', 'message_poll', 'draft_message', 'quick_replies',
        'new_quick_reply', 'delete_quick_reply', 'quick_reply_message', 'delete_quick_reply_messages',
        'contacts_reset', 'dialog_pinned', 'pinned_dialogs', 'pinned_messages', 'dialog_unread_mark',
        'peer_settings', 'peer_story_blocked', 'delete_messages', 'dialog_filter', 'dialog_filter_order',
        'dialog_filters', 'folder_peers', 'channel_view_forum_as_messages',
        'channel_state', 'saved_dialog_pinned', 'pinned_saved_dialogs', 'story', 'read_stories',
        'sent_story_reaction', 'new_story_reaction', 'noop',
        'read_channel_discussion_inbox', 'read_channel_discussion_outbox'
    ]::text[])
);
