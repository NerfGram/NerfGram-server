// Command giftassetseed downloads star-gift TGS assets from the TelegramGiftsAssests
// GitHub CDN and patches data/official-gifts for FluxGram imports.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iamxvbaba/td/bin"
	"github.com/iamxvbaba/td/tg"

	stargiftapp "telesrv/internal/app/stargifts"
)

const defaultCDN = "https://cdn.jsdelivr.net/gh/ssamy2/TelegramGiftsAssests@main"

var classicGiftIDs = []int64{
	5170145012310081615,
	5170233102089322756,
	5168103777563050263,
	5170250947678437525,
	5170144170496491616,
	5170314324215857265,
	5170564780938756245,
	6028601630662853006,
	5168043875654172773,
	5170521118301225164,
	5170690322832818290,
}

var nftGiftIDs = []int64{
	6005564615793050414,
	5870972044522291836,
	5936013938331222567,
	5936017773737018241,
}

type giftsDetails struct {
	Upgraded []struct {
		ShortName string `json:"short_name"`
		RegularID string `json:"regular_id"`
	} `json:"upgraded"`
}

type modelAsset struct {
	Name    string  `json:"name"`
	ModelID string  `json:"model_id"`
	TGSPath string  `json:"tgs_path"`
	Rarity  float64 `json:"rarity_permille"`
}

type rarityManifest struct {
	Kind     string `json:"kind"`
	Permille *int   `json:"permille,omitempty"`
}

type documentManifest struct {
	ID                 int64  `json:"id"`
	FileName           string `json:"file_name"`
	AnimationValidated bool   `json:"animation_validated,omitempty"`
	File               struct {
		Path   string `json:"path"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	} `json:"file"`
}

type giftManifest struct {
	Index       int     `json:"index"`
	Kind        string  `json:"kind"`
	ID          int64   `json:"id"`
	Title       string  `json:"title,omitempty"`
	Stars       int64   `json:"stars"`
	ConvertStars int64  `json:"convert_stars,omitempty"`
	UpgradeStars int64  `json:"upgrade_stars,omitempty"`
	DocumentIDs []int64 `json:"document_ids"`
}

type upgradeSetManifest struct {
	GiftID         int64            `json:"gift_id"`
	AttributeCount int              `json:"attribute_count"`
	Models         []modelEntry     `json:"models"`
	Patterns       []patternEntry   `json:"patterns"`
	Backdrops      []backdropEntry  `json:"backdrops"`
}

type modelEntry struct {
	Name       string         `json:"name"`
	DocumentID int64          `json:"document_id"`
	Crafted    bool           `json:"crafted,omitempty"`
	Rarity     rarityManifest `json:"rarity"`
}

type patternEntry struct {
	Name       string         `json:"name"`
	DocumentID int64          `json:"document_id"`
	Rarity     rarityManifest `json:"rarity"`
}

type backdropEntry struct {
	Name         string         `json:"name"`
	BackdropID   int            `json:"backdrop_id"`
	CenterColor  int            `json:"center_color"`
	EdgeColor    int            `json:"edge_color"`
	PatternColor int            `json:"pattern_color"`
	TextColor    int            `json:"text_color"`
	Rarity       rarityManifest `json:"rarity"`
}

type catalogManifest struct {
	Schema                   int                  `json:"schema"`
	Hash                     int                  `json:"hash"`
	RawCatalog               fileArtifact         `json:"raw_catalog"`
	GiftCount                int                  `json:"gift_count"`
	UpgradeAttributeSetCount int                  `json:"upgrade_attribute_set_count"`
	Gifts                    []giftManifest       `json:"gifts"`
	UpgradeAttributeSets     []upgradeSetManifest `json:"upgrade_attribute_sets"`
	Documents                []documentManifest   `json:"documents"`
	TotalBytes               int64                `json:"total_document_bytes"`
	BoundaryNote             string               `json:"boundary_note"`
}

type fileArtifact struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func main() {
	out := flag.String("out", "data/official-gifts", "official gifts directory")
	cdn := flag.String("cdn", defaultCDN, "TelegramGiftsAssests CDN base URL")
	flag.Parse()
	if err := run(strings.TrimRight(*cdn, "/"), filepath.Clean(*out)); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run(cdnBase, outDir string) error {
	client := &http.Client{Timeout: 2 * time.Minute}
	details, err := fetchGiftsDetails(client, cdnBase)
	if err != nil {
		return err
	}
	shortByGift := map[int64]string{}
	for _, item := range details.Upgraded {
		id, err := strconv.ParseInt(item.RegularID, 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		shortByGift[id] = item.ShortName
	}

	catalog, catalogArtifact, err := loadCatalog(outDir)
	if err != nil {
		return err
	}
	giftDoc := map[int64]int64{}
	for _, class := range catalog.Gifts {
		gift, ok := class.(*tg.StarGift)
		if !ok {
			continue
		}
		doc, ok := gift.Sticker.(*tg.Document)
		if !ok || doc.ID == 0 {
			continue
		}
		giftDoc[gift.ID] = doc.ID
	}

	downloaded := 0
	for _, giftID := range classicGiftIDs {
		docID := giftDoc[giftID]
		if docID == 0 {
			fmt.Printf("[skip base] gift=%d missing from catalog.tl\n", giftID)
			continue
		}
		if ok, err := seedBase(client, cdnBase, outDir, giftID, docID); err != nil {
			return err
		} else if ok {
			downloaded++
		}
	}
	for _, giftID := range nftGiftIDs {
		docID := giftDoc[giftID]
		if docID == 0 {
			fmt.Printf("[skip base] gift=%d missing from catalog.tl\n", giftID)
			continue
		}
		if ok, err := seedBase(client, cdnBase, outDir, giftID, docID); err != nil {
			return err
		} else if ok {
			downloaded++
		}
		short := shortByGift[giftID]
		if short == "" {
			continue
		}
		n, err := seedModels(client, cdnBase, outDir, short, giftID)
		if err != nil {
			return err
		}
		downloaded += n
	}

	manifest, err := rebuildManifest(outDir, catalog, catalogArtifact)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "manifest.json"), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("[complete] downloaded=%d gifts=%d collectible_sets=%d documents=%d bytes=%d\n",
		downloaded, manifest.GiftCount, manifest.UpgradeAttributeSetCount, len(manifest.Documents), manifest.TotalBytes)
	return nil
}

func fetchGiftsDetails(client *http.Client, cdnBase string) (giftsDetails, error) {
	var details giftsDetails
	if err := downloadJSON(client, cdnBase+"/Gifts_Details.json", &details); err != nil {
		return details, err
	}
	return details, nil
}

func seedBase(client *http.Client, cdnBase, outDir string, giftID, docID int64) (bool, error) {
	dest := filepath.Join(outDir, "documents", fmt.Sprintf("%d.tgs", docID))
	if _, err := os.Stat(dest); err == nil {
		return false, nil
	}
	url := fmt.Sprintf("%s/tgs/by_id/%d.tgs", cdnBase, giftID)
	return true, downloadDocument(client, url, dest, docID)
}

func seedModels(client *http.Client, cdnBase, outDir, shortName string, giftID int64) (int, error) {
	needed, err := missingModelNames(outDir, giftID)
	if err != nil {
		return 0, err
	}
	if len(needed) == 0 {
		return 0, nil
	}
	var models []modelAsset
	if err := downloadJSON(client, fmt.Sprintf("%s/models/%s/config.json", cdnBase, shortName), &models); err != nil {
		return 0, fmt.Errorf("models config %s: %w", shortName, err)
	}
	byID := map[string]string{}
	byName := map[string]string{}
	for _, model := range models {
		if model.ModelID != "" && model.TGSPath != "" {
			byID[model.ModelID] = model.TGSPath
		}
		if model.Name != "" && model.TGSPath != "" {
			byName[normalizeName(model.Name)] = model.TGSPath
		}
	}
	count := 0
	for docID, name := range needed {
		dest := filepath.Join(outDir, "documents", fmt.Sprintf("%d.tgs", docID))
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		var tgsPath string
		if path, ok := byID[strconv.FormatInt(docID, 10)]; ok {
			tgsPath = path
		} else if path, ok := byName[normalizeName(name)]; ok {
			tgsPath = path
		} else {
			tgsPath = fmt.Sprintf("models/%s/%s.tgs", shortName, slugFileName(name))
		}
		url := cdnBase + "/" + strings.TrimPrefix(tgsPath, "/")
		if err := downloadDocument(client, url, dest, docID); err != nil {
			fmt.Printf("[warn model] %s doc=%d url=%s: %v\n", name, docID, url, err)
			continue
		}
		count++
	}
	fmt.Printf("[models] %s downloaded=%d\n", shortName, count)
	return count, nil
}

func missingModelNames(outDir string, giftID int64) (map[int64]string, error) {
	raw, err := os.ReadFile(filepath.Join(outDir, "upgrade-attributes", fmt.Sprintf("%d.tl", giftID)))
	if err != nil {
		return nil, err
	}
	var attrs tg.PaymentsStarGiftUpgradeAttributes
	if err := attrs.Decode(&bin.Buffer{Buf: raw}); err != nil {
		return nil, err
	}
	out := map[int64]string{}
	for _, attr := range attrs.Attributes {
		model, ok := attr.(*tg.StarGiftAttributeModel)
		if !ok {
			continue
		}
		doc, ok := model.Document.(*tg.Document)
		if !ok || documentExists(outDir, doc.ID) {
			continue
		}
		out[doc.ID] = model.Name
	}
	return out, nil
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func slugFileName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '\'':
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func downloadJSON(client *http.Client, url string, target any) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func downloadDocument(client *http.Client, url, dest string, docID int64) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	validator := &stargiftapp.Service{}
	if _, err := validator.PrepareOfficialAnimation(fmt.Sprintf("%d.tgs", docID), data); err != nil {
		return fmt.Errorf("invalid tgs: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}

func loadCatalog(outDir string) (*tg.PaymentsStarGifts, fileArtifact, error) {
	path := filepath.Join(outDir, "catalog.tl")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fileArtifact{}, err
	}
	sum := sha256.Sum256(raw)
	artifact := fileArtifact{Kind: "tl", Path: "catalog.tl", Size: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])}
	var catalog tg.PaymentsStarGifts
	if err := catalog.Decode(&bin.Buffer{Buf: raw}); err != nil {
		return nil, artifact, err
	}
	return &catalog, artifact, nil
}

func rebuildManifest(outDir string, catalog *tg.PaymentsStarGifts, catalogArtifact fileArtifact) (catalogManifest, error) {
	manifest := catalogManifest{
		Schema:       2,
		Hash:         catalog.Hash,
		RawCatalog:   catalogArtifact,
		BoundaryNote: "patched with cmd/giftassetseed from ssamy2/TelegramGiftsAssests",
	}
	docIDs := map[int64]struct{}{}

	for index, class := range catalog.Gifts {
		gift, ok := class.(*tg.StarGift)
		if !ok || gift.ID <= 0 || gift.Stars <= 0 {
			continue
		}
		doc, ok := gift.Sticker.(*tg.Document)
		if !ok || doc.ID <= 0 {
			continue
		}
		if !documentExists(outDir, doc.ID) {
			continue
		}
		docIDs[doc.ID] = struct{}{}
		manifest.Gifts = append(manifest.Gifts, giftManifest{
			Index:        index,
			Kind:         "regular",
			ID:           gift.ID,
			Title:        gift.Title,
			Stars:        gift.Stars,
			ConvertStars: gift.ConvertStars,
			UpgradeStars: gift.UpgradeStars,
			DocumentIDs:  []int64{doc.ID},
		})
	}

	entries, err := os.ReadDir(filepath.Join(outDir, "upgrade-attributes"))
	if err != nil {
		return manifest, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tl") {
			continue
		}
		giftID, err := strconv.ParseInt(strings.TrimSuffix(entry.Name(), ".tl"), 10, 64)
		if err != nil || giftID <= 0 {
			continue
		}
		set, needed, err := loadUpgradeSet(outDir, giftID)
		if err != nil {
			return manifest, err
		}
		if !setComplete(outDir, needed) {
			continue
		}
		for id := range needed {
			docIDs[id] = struct{}{}
		}
		manifest.UpgradeAttributeSets = append(manifest.UpgradeAttributeSets, set)
	}
	sort.Slice(manifest.Gifts, func(i, j int) bool { return manifest.Gifts[i].Index < manifest.Gifts[j].Index })
	sort.Slice(manifest.UpgradeAttributeSets, func(i, j int) bool { return manifest.UpgradeAttributeSets[i].GiftID < manifest.UpgradeAttributeSets[j].GiftID })
	manifest.GiftCount = len(manifest.Gifts)
	manifest.UpgradeAttributeSetCount = len(manifest.UpgradeAttributeSets)

	ids := make([]int64, 0, len(docIDs))
	for id := range docIDs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		doc, err := documentManifestFor(outDir, id)
		if err != nil {
			return manifest, err
		}
		manifest.Documents = append(manifest.Documents, doc)
		manifest.TotalBytes += doc.File.Size
	}
	return manifest, nil
}

func loadUpgradeSet(outDir string, giftID int64) (upgradeSetManifest, map[int64]struct{}, error) {
	raw, err := os.ReadFile(filepath.Join(outDir, "upgrade-attributes", fmt.Sprintf("%d.tl", giftID)))
	if err != nil {
		return upgradeSetManifest{}, nil, err
	}
	var attrs tg.PaymentsStarGiftUpgradeAttributes
	if err := attrs.Decode(&bin.Buffer{Buf: raw}); err != nil {
		return upgradeSetManifest{}, nil, err
	}
	set := upgradeSetManifest{GiftID: giftID}
	needed := map[int64]struct{}{}
	for _, attr := range attrs.Attributes {
		switch value := attr.(type) {
		case *tg.StarGiftAttributeModel:
			doc, ok := value.Document.(*tg.Document)
			if !ok {
				continue
			}
			needed[doc.ID] = struct{}{}
			rarity, err := rarityFrom(value.Rarity, value.GetCrafted())
			if err != nil {
				return upgradeSetManifest{}, nil, err
			}
			set.Models = append(set.Models, modelEntry{Name: value.Name, DocumentID: doc.ID, Crafted: value.GetCrafted(), Rarity: rarity})
		case *tg.StarGiftAttributePattern:
			doc, ok := value.Document.(*tg.Document)
			if !ok {
				continue
			}
			needed[doc.ID] = struct{}{}
			rarity, err := rarityFrom(value.Rarity, false)
			if err != nil {
				return upgradeSetManifest{}, nil, err
			}
			set.Patterns = append(set.Patterns, patternEntry{Name: value.Name, DocumentID: doc.ID, Rarity: rarity})
		case *tg.StarGiftAttributeBackdrop:
			rarity, err := rarityFrom(value.Rarity, false)
			if err != nil {
				return upgradeSetManifest{}, nil, err
			}
			set.Backdrops = append(set.Backdrops, backdropEntry{
				Name: value.Name, BackdropID: value.BackdropID,
				CenterColor: value.CenterColor, EdgeColor: value.EdgeColor,
				PatternColor: value.PatternColor, TextColor: value.TextColor, Rarity: rarity,
			})
		}
	}
	set.AttributeCount = len(set.Models) + len(set.Patterns) + len(set.Backdrops)
	return set, needed, nil
}

func rarityFrom(class tg.StarGiftAttributeRarityClass, crafted bool) (rarityManifest, error) {
	switch value := class.(type) {
	case *tg.StarGiftAttributeRarity:
		if crafted {
			return rarityManifest{}, fmt.Errorf("crafted model cannot use permille rarity")
		}
		p := value.Permille
		return rarityManifest{Kind: "permille", Permille: &p}, nil
	case *tg.StarGiftAttributeRarityUncommon:
		return rarityManifest{Kind: "uncommon"}, nil
	case *tg.StarGiftAttributeRarityRare:
		return rarityManifest{Kind: "rare"}, nil
	case *tg.StarGiftAttributeRarityEpic:
		return rarityManifest{Kind: "epic"}, nil
	case *tg.StarGiftAttributeRarityLegendary:
		return rarityManifest{Kind: "legendary"}, nil
	default:
		return rarityManifest{}, fmt.Errorf("unsupported rarity %T", class)
	}
}

func setComplete(outDir string, needed map[int64]struct{}) bool {
	if len(needed) == 0 {
		return false
	}
	for id := range needed {
		if !documentExists(outDir, id) {
			return false
		}
	}
	return true
}

func documentExists(outDir string, id int64) bool {
	_, err := os.Stat(filepath.Join(outDir, "documents", fmt.Sprintf("%d.tgs", id)))
	return err == nil
}

func documentManifestFor(outDir string, id int64) (documentManifest, error) {
	rel := filepath.Join("documents", fmt.Sprintf("%d.tgs", id))
	data, err := os.ReadFile(filepath.Join(outDir, rel))
	if err != nil {
		return documentManifest{}, err
	}
	sum := sha256.Sum256(data)
	validator := &stargiftapp.Service{}
	validated := false
	if _, err := validator.PrepareOfficialAnimation(fmt.Sprintf("%d.tgs", id), data); err == nil {
		validated = true
	}
	return documentManifest{
		ID: id, FileName: fmt.Sprintf("%d.tgs", id), AnimationValidated: validated,
		File: struct {
			Path   string `json:"path"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		}{Path: filepath.ToSlash(rel), Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])},
	}, nil
}
