package tdesktop

import (
	"sync"

	"telesrv/internal/seed/appearance"

	"github.com/iamxvbaba/td/tg"
)

const appearanceSeedDCID = 2
const maxPeerColorBoostLevel = 100

var peerColorOptionsCache = struct {
	regularOnce sync.Once
	profileOnce sync.Once
	regular     []tg.HelpPeerColorOption
	profile     []tg.HelpPeerColorOption
}{}

func DefaultWallPapers() []tg.WallPaperClass {
	catalog := appearance.Default()
	out := make([]tg.WallPaperClass, 0, len(catalog.Wallpapers))
	for _, wallpaper := range catalog.Wallpapers {
		out = append(out, DefaultWallPaper(wallpaper))
	}
	return out
}

// LookupWallPaper resolves a cloud wallpaper from the Default seed catalog.
func LookupWallPaper(input tg.InputWallPaperClass) (tg.WallPaperClass, bool) {
	if in, ok := input.(*tg.InputWallPaperNoFile); ok {
		return &tg.WallPaperNoFile{ID: in.ID}, true
	}
	catalog := appearance.Default()
	for _, wallpaper := range catalog.Wallpapers {
		if inputWallPaperMatches(input, wallpaper) {
			return DefaultWallPaper(wallpaper), true
		}
	}
	// account.getThemes/account.getChatThemes also advertise wallpapers nested
	// in ThemeSettings. The default getWallPapers export is a filtered list and
	// does not contain every nested entry, so these identities must remain part
	// of the same lookup boundary or Android can render a theme that it cannot
	// subsequently install.
	for _, theme := range catalog.ChatThemes {
		for _, settings := range theme.Settings {
			if inputWallPaperMatches(input, settings.Wallpaper) {
				return DefaultWallPaper(settings.Wallpaper), true
			}
		}
	}
	// DrKLO normally installs a default theme with the nested wallpaper slug
	// stored on ThemeAccent. During accent restoration it can instead fall back
	// to ThemeInfo.slug, which is the slug of the exact Theme advertised by
	// account.getThemes. Accept that server-issued alias only when every setting
	// of the matched theme points at one unambiguous file wallpaper.
	if in, ok := input.(*tg.InputWallPaperSlug); ok {
		if wallpaper, ok := lookupChatThemeWallpaperAlias(catalog.ChatThemes, in.Slug); ok {
			return DefaultWallPaper(wallpaper), true
		}
	}
	return nil, false
}

func lookupChatThemeWallpaperAlias(themes []appearance.ChatTheme, slug string) (appearance.Wallpaper, bool) {
	if slug == "" {
		return appearance.Wallpaper{}, false
	}
	var resolved appearance.Wallpaper
	found := false
	for _, theme := range themes {
		if theme.Slug != slug || len(theme.Settings) == 0 {
			continue
		}
		var themeWallpaper appearance.Wallpaper
		for i, settings := range theme.Settings {
			wallpaper := settings.Wallpaper
			if wallpaper.Slug == "" || wallpaper.ID == 0 {
				return appearance.Wallpaper{}, false
			}
			if i == 0 {
				themeWallpaper = wallpaper
				continue
			}
			if !sameWallpaperIdentity(themeWallpaper, wallpaper) {
				return appearance.Wallpaper{}, false
			}
		}
		if found && !sameWallpaperIdentity(resolved, themeWallpaper) {
			return appearance.Wallpaper{}, false
		}
		resolved = themeWallpaper
		found = true
	}
	return resolved, found
}

func sameWallpaperIdentity(a, b appearance.Wallpaper) bool {
	return a.ID == b.ID && a.AccessHash == b.AccessHash && a.Slug == b.Slug
}

// LookupWallPapers resolves multiple wallpapers from the Default seed catalog.
func LookupWallPapers(inputs []tg.InputWallPaperClass) ([]tg.WallPaperClass, bool) {
	out := make([]tg.WallPaperClass, 0, len(inputs))
	for _, input := range inputs {
		wallpaper, ok := LookupWallPaper(input)
		if !ok {
			return nil, false
		}
		out = append(out, wallpaper)
	}
	return out, true
}

func inputWallPaperMatches(input tg.InputWallPaperClass, wallpaper appearance.Wallpaper) bool {
	switch in := input.(type) {
	case *tg.InputWallPaper:
		return in.ID == wallpaper.ID && in.AccessHash == wallpaper.AccessHash
	case *tg.InputWallPaperSlug:
		return in.Slug != "" && in.Slug == wallpaper.Slug
	default:
		return false
	}
}

func DefaultWallPaper(in appearance.Wallpaper) tg.WallPaperClass {
	if in.Type == 1 || in.Document.ID == 0 {
		out := &tg.WallPaperNoFile{ID: in.ID}
		out.SetDefault(in.Default)
		out.SetDark(in.Dark)
		out.SetSettings(DefaultWallPaperSettings(in.Settings))
		return out
	}
	out := &tg.WallPaper{
		ID:         in.ID,
		AccessHash: in.AccessHash,
		Slug:       in.Slug,
		Document:   DefaultDocument(in.Document),
	}
	out.SetDefault(in.Default)
	out.SetPattern(in.Pattern)
	out.SetDark(in.Dark)
	out.SetSettings(DefaultWallPaperSettings(in.Settings))
	return out
}

func DefaultWallPaperSettings(in appearance.WallpaperSettings) tg.WallPaperSettings {
	var out tg.WallPaperSettings
	out.SetBlur(in.Blur)
	out.SetMotion(in.Motion)
	if in.BackgroundColor != 0 {
		out.SetBackgroundColor(in.BackgroundColor)
	}
	if in.SecondBackgroundColor != 0 {
		out.SetSecondBackgroundColor(in.SecondBackgroundColor)
	}
	if in.ThirdBackgroundColor != 0 {
		out.SetThirdBackgroundColor(in.ThirdBackgroundColor)
	}
	if in.FourthBackgroundColor != 0 {
		out.SetFourthBackgroundColor(in.FourthBackgroundColor)
	}
	if in.Intensity != 0 {
		out.SetIntensity(in.Intensity)
	}
	if in.Rotation != 0 {
		out.SetRotation(in.Rotation)
	}
	return out
}

func DefaultDocument(in appearance.Document) tg.DocumentClass {
	if in.ID == 0 {
		return &tg.DocumentEmpty{}
	}
	doc := appearanceDocumentForClients(in)
	return &tg.Document{
		ID:            doc.ID,
		AccessHash:    doc.AccessHash,
		Date:          doc.Date,
		MimeType:      doc.MimeType,
		Size:          doc.Size,
		Thumbs:        DefaultPhotoSizes(in.Thumbs, doc),
		DCID:          appearanceSeedDCID,
		Attributes:    DefaultDocumentAttributes(doc.Attributes),
		FileReference: nil,
	}
}

// appearanceDocumentForClients rewrites gzip-SVG wall patterns as PNG using the
// catalog thumb. Android ImageLoader treats application/x-tgwallpattern thumbs
// as SVG and fails to decode the PNG preview; desktop accepts PNG patterns.
func appearanceDocumentForClients(in appearance.Document) appearance.Document {
	if in.MimeType != "application/x-tgwallpattern" || len(in.Thumbs) == 0 {
		return in
	}
	thumb := in.Thumbs[0]
	if thumb.Path == "" || thumb.Size <= 0 || thumb.W <= 0 || thumb.H <= 0 {
		return in
	}
	out := in
	out.MimeType = "image/png"
	out.Size = int64(thumb.Size)
	out.Path = thumb.Path
	out.SHA256 = thumb.SHA256
	out.Attributes = []appearance.DocumentAttribute{
		{Kind: "image_size", W: thumb.W, H: thumb.H},
		{Kind: "filename", FileName: "pattern.png"},
	}
	return out
}

func DefaultPhotoSizes(in []appearance.PhotoSize, doc appearance.Document) []tg.PhotoSizeClass {
	out := make([]tg.PhotoSizeClass, 0, len(in))
	for _, size := range in {
		switch size.Kind {
		case "size":
			if size.Type == "" || size.W <= 0 || size.H <= 0 || size.Size <= 0 {
				continue
			}
			// Inline PNG thumb bytes so Android/Desktop preview without a second
			// getFile round-trip (and without SVG mis-decode of the thumb).
			if doc.MimeType == "image/png" && size.Path != "" {
				if data, err := appearance.FS.ReadFile(size.Path); err == nil && len(data) > 0 {
					out = append(out, &tg.PhotoCachedSize{
						Type:  size.Type,
						W:     size.W,
						H:     size.H,
						Bytes: append([]byte(nil), data...),
					})
					continue
				}
			}
			out = append(out, &tg.PhotoSize{Type: size.Type, W: size.W, H: size.H, Size: size.Size})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func DefaultDocumentAttributes(in []appearance.DocumentAttribute) []tg.DocumentAttributeClass {
	out := make([]tg.DocumentAttributeClass, 0, len(in))
	for _, attr := range in {
		switch attr.Kind {
		case "image_size":
			out = append(out, &tg.DocumentAttributeImageSize{W: attr.W, H: attr.H})
		case "filename":
			if attr.FileName != "" {
				out = append(out, &tg.DocumentAttributeFilename{FileName: attr.FileName})
			}
		}
	}
	return out
}

func DefaultPeerColorOptions(profile bool) []tg.HelpPeerColorOption {
	if profile {
		peerColorOptionsCache.profileOnce.Do(func() {
			peerColorOptionsCache.profile = buildDefaultPeerColorOptions(true)
		})
		return clonePeerColorOptions(peerColorOptionsCache.profile)
	}
	peerColorOptionsCache.regularOnce.Do(func() {
		peerColorOptionsCache.regular = buildDefaultPeerColorOptions(false)
	})
	return clonePeerColorOptions(peerColorOptionsCache.regular)
}

func buildDefaultPeerColorOptions(profile bool) []tg.HelpPeerColorOption {
	catalog := appearance.Default()
	source := catalog.PeerColors
	if profile {
		source = catalog.PeerProfileColors
	}
	out := make([]tg.HelpPeerColorOption, 0, len(source))
	for _, color := range source {
		option := tg.HelpPeerColorOption{ColorID: color.ID}
		option.SetHidden(color.Hidden)
		channelMin := boundedPeerColorMinLevel(color.ChannelMinLevel)
		if channelMin > 0 {
			option.SetChannelMinLevel(channelMin)
		}
		groupMin := boundedPeerColorMinLevel(color.GroupMinLevel)
		if groupMin == 0 && profile {
			groupMin = channelMin
		}
		if groupMin > 0 {
			option.SetGroupMinLevel(groupMin)
		}
		if colors := DefaultPeerColorSet(color.Colors); colors != nil {
			option.SetColors(colors)
		}
		if colors := DefaultPeerColorSet(color.DarkColors); colors != nil {
			option.SetDarkColors(colors)
		}
		out = append(out, option)
	}
	return out
}

func clonePeerColorOptions(in []tg.HelpPeerColorOption) []tg.HelpPeerColorOption {
	if len(in) == 0 {
		return nil
	}
	out := make([]tg.HelpPeerColorOption, len(in))
	for i := range in {
		out[i] = in[i]
		if in[i].Colors != nil {
			out[i].Colors = clonePeerColorSet(in[i].Colors)
		}
		if in[i].DarkColors != nil {
			out[i].DarkColors = clonePeerColorSet(in[i].DarkColors)
		}
	}
	return out
}

func clonePeerColorSet(in tg.HelpPeerColorSetClass) tg.HelpPeerColorSetClass {
	switch set := in.(type) {
	case *tg.HelpPeerColorSet:
		return &tg.HelpPeerColorSet{Colors: append([]int(nil), set.Colors...)}
	case *tg.HelpPeerColorProfileSet:
		return &tg.HelpPeerColorProfileSet{
			PaletteColors: append([]int(nil), set.PaletteColors...),
			BgColors:      append([]int(nil), set.BgColors...),
			StoryColors:   append([]int(nil), set.StoryColors...),
		}
	default:
		return in
	}
}

func boundedPeerColorMinLevel(level int) int {
	if level <= 0 {
		return 0
	}
	if level > maxPeerColorBoostLevel {
		return maxPeerColorBoostLevel
	}
	return level
}

func DefaultPeerColorID(id int, profile bool) (bool, bool) {
	catalog := appearance.Default()
	source := catalog.PeerColors
	if profile {
		source = catalog.PeerProfileColors
	}
	if len(source) == 0 {
		return false, false
	}
	for _, color := range source {
		if color.ID == id {
			return true, true
		}
	}
	return false, true
}

func DefaultPeerColorSet(in *appearance.ColorSet) tg.HelpPeerColorSetClass {
	if in == nil {
		return nil
	}
	if len(in.PaletteColors) > 0 || len(in.BgColors) > 0 || len(in.StoryColors) > 0 {
		return &tg.HelpPeerColorProfileSet{
			PaletteColors: append([]int(nil), in.PaletteColors...),
			BgColors:      append([]int(nil), in.BgColors...),
			StoryColors:   append([]int(nil), in.StoryColors...),
		}
	}
	if len(in.Colors) > 0 {
		return &tg.HelpPeerColorSet{Colors: append([]int(nil), in.Colors...)}
	}
	return nil
}
