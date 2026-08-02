package files

import (
	"context"
	"fmt"
	"strings"

	"telesrv/internal/domain"
)

// 合成集的固定 ID/AccessHash：种子导出的真实集 ID 在 1.2e15 量级，取明显隔离的
// 常量段避免撞键；幂等性由 system_key 查询保证，常量仅需稳定。
const (
	defaultEmojiStatusSetID         int64 = 7_777_000_000_000_001
	defaultEmojiStatusSetAccessHash int64 = 7_777_000_000_000_002
)

// defaultEmojiStatusEmoticons 是默认状态的精选 emoji（对齐官方默认状态选盘的
// 常见项），按展示顺序排列。匹配不到的 emoticon 静默跳过（取决于 seed 内容）。
var defaultEmojiStatusEmoticons = []string{
	"\U0001f4bc", // 💼 工作
	"\U0001f393", // 🎓 学习
	"\U0001f3e0", // 🏠 在家
	"\U0001f334", // 🌴 度假
	"\U0001f3d6", // 🏖 海滩
	"✈",          // ✈️ 旅行
	"\U0001f912", // 🤒 生病
	"\U0001f634", // 😴 睡觉
	"☕",          // ☕ 咖啡
	"\U0001f4bb", // 💻 编码/办公
	"\U0001f4da", // 📚 阅读
	"\U0001f3ae", // 🎮 游戏
	"\U0001f3a7", // 🎧 听歌
	"⚽",          // ⚽ 运动
	"\U0001f3c6", // 🏆 获胜
	"❤",          // ❤️ 爱心
	"\U0001f60e", // 😎 酷
	"\U0001f319", // 🌙 勿扰
	"⭐",          // ⭐ 星标
	"\U0001f525", // 🔥 火
	"\U0001f44d", // 👍 赞
	"\U0001f389", // 🎉 庆祝
	"\U0001f914", // 🤔 思考
	"\U0001f607", // 😇 天使
	"\U0001f973", // 🥳 派对
	"\U0001f602", // 😂 大笑
	"\U0001f970", // 🥰 喜爱
	"\U0001f62d", // 😭 大哭
	"\U0001f92f", // 🤯 爆炸
	"\U0001f440", // 👀 围观
	"\U0001f4af", // 💯 满分
	"\U0001f64f", // 🙏 感谢
	"\U0001f91d", // 🤝 合作
	"✍",          // ✍️ 写作
	"\U0001f697", // 🚗 通勤
	"\U0001f355", // 🍕 干饭
	"\U0001f382", // 🎂 生日
	"\U0001f338", // 🌸 春天
	"⛄",          // ⛄ 冬天
	"\U0001f984", // 🦄 独角兽
}

// EnsureDefaultEmojiStatusSet 幂等地合成默认 emoji status 系统集：从已 seed 的
// animated_emoji 系统集按 emoticon 精选文档（复用文档行与 blob，不复制字节）。
// 返回 (集内文档数, 是否本次新建)。animated_emoji 未 seed 时静默跳过。
func (s *Service) EnsureDefaultEmojiStatusSet(ctx context.Context) (int, bool, error) {
	if existing, found, err := s.media.GetStickerSetBySystemKey(ctx, domain.StickerSetSystemKeyEmojiDefaultStatuses); err != nil {
		return 0, false, fmt.Errorf("lookup default emoji status set: %w", err)
	} else if found {
		return len(existing.DocumentIDs), false, nil
	}
	source, found, err := s.media.GetStickerSetBySystemKey(ctx, "animated_emoji")
	if err != nil {
		return 0, false, fmt.Errorf("lookup animated_emoji set: %w", err)
	}
	if !found || len(source.Packs) == 0 {
		return 0, false, nil
	}
	byEmoticon := make(map[string][]int64, len(source.Packs))
	for _, pack := range source.Packs {
		key := normalizeStatusEmoticon(pack.Emoticon)
		if key == "" || len(pack.DocumentIDs) == 0 {
			continue
		}
		byEmoticon[key] = append(byEmoticon[key], pack.DocumentIDs...)
	}
	set := domain.StickerSet{
		ID:         defaultEmojiStatusSetID,
		AccessHash: defaultEmojiStatusSetAccessHash,
		ShortName:  "FromGramDefaultStatuses",
		Title:      "Default Emoji Statuses",
		Kind:       domain.StickerSetKindSystem,
		SystemKey:  domain.StickerSetSystemKeyEmojiDefaultStatuses,
		Official:   true,
		Animated:   source.Animated,
		Emojis:     true,
	}
	seen := make(map[int64]struct{})
	for _, emoticon := range defaultEmojiStatusEmoticons {
		ids := byEmoticon[normalizeStatusEmoticon(emoticon)]
		if len(ids) == 0 {
			continue
		}
		pack := domain.StickerPack{Emoticon: emoticon}
		for _, id := range ids {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			set.DocumentIDs = append(set.DocumentIDs, id)
			pack.DocumentIDs = append(pack.DocumentIDs, id)
		}
		if len(pack.DocumentIDs) > 0 {
			set.Packs = append(set.Packs, pack)
		}
	}
	if len(set.DocumentIDs) == 0 {
		return 0, false, nil
	}
	set.Count = len(set.DocumentIDs)
	set.Hash = stickerSetDocsHash(set.DocumentIDs)
	docs, err := s.media.GetDocuments(ctx, set.DocumentIDs)
	if err != nil {
		return 0, false, fmt.Errorf("load default emoji status documents: %w", err)
	}
	if err := s.media.PutStickerSet(ctx, set); err != nil {
		return 0, false, fmt.Errorf("persist default emoji status set: %w", err)
	}
	s.stickerSetCache.put(set, orderDocuments(docs, set.DocumentIDs))
	return set.Count, true, nil
}

// normalizeStatusEmoticon 统一变体选择符差异（"❤" vs "❤️"），匹配 seed packs 与
// 精选清单两侧的书写形态。
func normalizeStatusEmoticon(e string) string {
	return strings.ReplaceAll(strings.TrimSpace(e), "️", "")
}

// stickerSetDocsHash 由文档 ID 列表算稳定 set hash（messages.getStickerSet 与
// account.getDefaultEmojiStatuses 共用一份缓存判定）。
func stickerSetDocsHash(ids []int64) int {
	var h uint64
	for _, id := range ids {
		h ^= uint64(id)
		h = h*0x4f25 + uint64(id)
	}
	return int(h & 0x7fffffff)
}
