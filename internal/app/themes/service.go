// Package themes 实现自定义云主题(account.createTheme/updateTheme/getTheme/
// saveTheme/installTheme)的业务编排:分配 id/access_hash/slug、校验创建者、维护每用户
// 已安装列表。文件上传(uploadTheme)由 rpc 层直接经 Files 服务完成,不经此服务。
package themes

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"strings"
	"time"

	"telesrv/internal/domain"
	"telesrv/internal/store"
)

// 自定义主题 id/access_hash 用 crypto 随机 63 位,刻意区别于静态目录的保留区间
// (静态 emoji 主题用 92e16/93e16),避免 install/get-by-id 串到静态目录。
const defaultSlugPrefix = "t-"

// slug 字母表:小写 + 数字,URL 安全。
const slugAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// Service 提供自定义云主题业务。
type Service struct {
	store      store.ThemeStore
	now        func() time.Time
	slugPrefix string
}

// Option 调整主题服务可选项。
type Option func(*Service)

// WithClock 注入时钟(测试用)。
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithSlugPrefix 覆盖自动分配 slug 的前缀。
func WithSlugPrefix(prefix string) Option {
	return func(s *Service) { s.slugPrefix = prefix }
}

// NewService 创建主题服务。
func NewService(st store.ThemeStore, opts ...Option) *Service {
	s := &Service{store: st, now: time.Now, slugPrefix: defaultSlugPrefix}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) ready() bool { return s != nil && s.store != nil }

// Create 创建一份新主题。spec.Slug 为空时自动分配;slug 冲突自动重试。
func (s *Service) Create(ctx context.Context, spec domain.ThemeSpec) (domain.Theme, error) {
	if !s.ready() {
		return domain.Theme{}, domain.ErrThemeInvalid
	}
	if spec.CreatorUserID == 0 {
		return domain.Theme{}, domain.ErrThemeInvalid
	}
	userSlug := strings.TrimSpace(spec.Slug)
	// 最多重试若干次(自动 slug/极小概率 id 冲突)。
	for attempt := 0; attempt < 8; attempt++ {
		t := domain.Theme{
			ID:            randInt63(),
			AccessHash:    randInt63(),
			CreatorUserID: spec.CreatorUserID,
			Title:         spec.Title,
			Emoticon:      spec.Emoticon,
			ForChat:       spec.ForChat,
			DocumentID:    spec.DocumentID,
			Settings:      spec.Settings,
			InstallsCount: 0,
			CreatedAt:     s.now().Unix(),
		}
		if userSlug != "" {
			t.Slug = userSlug
		} else {
			t.Slug = s.slugPrefix + randSlug(12)
		}
		err := s.store.CreateTheme(ctx, t)
		switch {
		case err == nil:
			return t.Clone(), nil
		case err == domain.ErrThemeSlugTaken && userSlug != "":
			// 用户指定的 slug 已被占用:不重试,直接报错。
			return domain.Theme{}, domain.ErrThemeSlugTaken
		case err == domain.ErrThemeSlugTaken:
			continue // 自动 slug/id 冲突:换一组重试
		default:
			return domain.Theme{}, err
		}
	}
	return domain.Theme{}, domain.ErrThemeSlugTaken
}

func (s *Service) resolve(ctx context.Context, ref domain.ThemeRef) (domain.Theme, bool, error) {
	if ref.ID != 0 {
		t, ok, err := s.store.GetThemeByID(ctx, ref.ID)
		if err != nil || !ok {
			return domain.Theme{}, ok, err
		}
		if t.AccessHash != ref.AccessHash {
			return domain.Theme{}, false, nil
		}
		return t, true, nil
	}
	if ref.Slug != "" {
		return s.store.GetThemeBySlug(ctx, ref.Slug)
	}
	return domain.Theme{}, false, nil
}

// Get 按 id 或 slug 解析一份主题。
func (s *Service) Get(ctx context.Context, ref domain.ThemeRef) (domain.Theme, bool, error) {
	if !s.ready() {
		return domain.Theme{}, false, domain.ErrThemeInvalid
	}
	return s.resolve(ctx, ref)
}

// Update 更新一份主题(仅创建者可改);部分字段。
func (s *Service) Update(ctx context.Context, userID int64, ref domain.ThemeRef, upd domain.ThemeUpdate) (domain.Theme, error) {
	if !s.ready() {
		return domain.Theme{}, domain.ErrThemeInvalid
	}
	t, ok, err := s.resolve(ctx, ref)
	if err != nil {
		return domain.Theme{}, err
	}
	if !ok {
		return domain.Theme{}, domain.ErrThemeNotFound
	}
	if !t.IsCreator(userID) {
		return domain.Theme{}, domain.ErrThemeInvalid
	}
	if upd.Slug != nil {
		t.Slug = strings.TrimSpace(*upd.Slug)
	}
	if upd.Title != nil {
		t.Title = *upd.Title
	}
	if upd.DocumentID != nil {
		t.DocumentID = *upd.DocumentID
	}
	if upd.Settings != nil {
		t.Settings = *upd.Settings
	}
	if err := s.store.UpdateTheme(ctx, t); err != nil {
		return domain.Theme{}, err
	}
	return t.Clone(), nil
}

// Save 把主题加入用户已存列表(saveTheme,unsave=false)。
func (s *Service) Save(ctx context.Context, userID int64, ref domain.ThemeRef) error {
	return s.membership(ctx, userID, ref, false, false)
}

// Unsave 从用户列表移除(saveTheme,unsave=true)。
func (s *Service) Unsave(ctx context.Context, userID int64, ref domain.ThemeRef) error {
	if !s.ready() {
		return domain.ErrThemeInvalid
	}
	t, ok, err := s.resolve(ctx, ref)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrThemeInvalid
	}
	return s.store.RemoveInstalled(ctx, userID, t.ID)
}

// Install 应用主题:计数 +1 并加入用户已安装列表。
func (s *Service) Install(ctx context.Context, userID int64, ref domain.ThemeRef, dark bool) error {
	return s.membership(ctx, userID, ref, true, dark)
}

func (s *Service) membership(ctx context.Context, userID int64, ref domain.ThemeRef, increment, dark bool) error {
	if !s.ready() {
		return domain.ErrThemeInvalid
	}
	t, ok, err := s.resolve(ctx, ref)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrThemeInvalid
	}
	if increment {
		if err := s.store.IncrementInstalls(ctx, t.ID); err != nil {
			return err
		}
	}
	return s.store.SetInstalled(ctx, userID, t.ID, dark)
}

// ListInstalled 返回用户已安装/已存主题。
func (s *Service) ListInstalled(ctx context.Context, userID int64) ([]domain.Theme, error) {
	if !s.ready() {
		return nil, domain.ErrThemeInvalid
	}
	return s.store.ListInstalledByUser(ctx, userID)
}

// ListForUser 返回用户「创建 ∪ 安装」的主题,供 getThemes 跨设备同步。
func (s *Service) ListForUser(ctx context.Context, userID int64) ([]domain.Theme, error) {
	if !s.ready() {
		return nil, domain.ErrThemeInvalid
	}
	return s.store.ListThemesForUser(ctx, userID)
}

// randInt63 返回一个 crypto 随机的正 63 位整数。
func randInt63() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(binary.BigEndian.Uint64(b[:]) & 0x7fffffffffffffff)
}

// randSlug 返回 n 个字符的随机 slug 片段。
func randSlug(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	out := make([]byte, n)
	for i := range out {
		out[i] = slugAlphabet[int(b[i])%len(slugAlphabet)]
	}
	return string(out)
}
