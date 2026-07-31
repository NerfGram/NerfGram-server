// Command cleangiftmanifest removes failed document entries from a giftfetch
// manifest so the official-gifts catalog can load for admin imports.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type manifest struct {
	Schema               int                      `json:"schema"`
	Hash                 int                      `json:"hash"`
	RawCatalog           any                      `json:"raw_catalog"`
	GiftCount            int                      `json:"gift_count"`
	ChatCount            int                      `json:"chat_count"`
	UserCount            int                      `json:"user_count"`
	UpgradeableGiftCount int                      `json:"upgradeable_gift_count"`
	UpgradeAttributeSetCount int                  `json:"upgrade_attribute_set_count"`
	UpgradeAttributeCount    int                  `json:"upgrade_attribute_count"`
	UpgradeModelCount        int                  `json:"upgrade_model_count"`
	UpgradePatternCount      int                  `json:"upgrade_pattern_count"`
	UpgradeBackdropCount     int                  `json:"upgrade_backdrop_count"`
	MissingThumbCount        int                  `json:"missing_thumb_count"`
	Gifts                    []giftManifest       `json:"gifts"`
	UpgradeAttributeSets     []upgradeSetManifest `json:"upgrade_attribute_sets"`
	Documents                []documentManifest   `json:"documents"`
	TotalBytes               int64                `json:"total_document_bytes"`
	BoundaryNote             string               `json:"boundary_note"`
}

type giftManifest struct {
	Index       int     `json:"index"`
	Kind        string  `json:"kind"`
	ID          int64   `json:"id"`
	Title       string  `json:"title,omitempty"`
	Stars       int64   `json:"stars,omitempty"`
	DocumentIDs []int64 `json:"document_ids"`
}

type upgradeSetManifest struct {
	GiftID64        int64            `json:"gift_id"`
	AttributeCount  int              `json:"attribute_count"`
	Models          []attrManifest   `json:"models"`
	Patterns        []attrManifest   `json:"patterns"`
	Backdrops       []backdropManifest `json:"backdrops"`
}

type attrManifest struct {
	DocumentID int64 `json:"document_id"`
}

type backdropManifest struct {
	BackdropID int `json:"backdrop_id"`
}

type documentManifest struct {
	ID              int64  `json:"id"`
	ValidationError string `json:"validation_error,omitempty"`
	File            struct {
		Path   string `json:"path"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	} `json:"file"`
}

func main() {
	path := flag.String("manifest", "data/official-gifts/manifest.json", "manifest path")
	flag.Parse()
	raw, err := os.ReadFile(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	beforeDocs := len(m.Documents)
	valid := map[int64]documentManifest{}
	for _, doc := range m.Documents {
		if doc.ID <= 0 || doc.File.Size <= 0 || len(doc.File.SHA256) != 64 || strings.TrimSpace(doc.File.Path) == "" || strings.TrimSpace(doc.ValidationError) != "" {
			continue
		}
		valid[doc.ID] = doc
	}
	docs := make([]documentManifest, 0, len(valid))
	var total int64
	for _, doc := range valid {
		docs = append(docs, doc)
		total += doc.File.Size
	}
	m.Documents = docs

	gifts := make([]giftManifest, 0, len(m.Gifts))
	for _, gift := range m.Gifts {
		if gift.Kind != "regular" || gift.ID <= 0 || len(gift.DocumentIDs) != 1 {
			continue
		}
		if _, ok := valid[gift.DocumentIDs[0]]; !ok {
			continue
		}
		gifts = append(gifts, gift)
	}
	m.Gifts = gifts
	m.GiftCount = len(gifts)

	sets := make([]upgradeSetManifest, 0, len(m.UpgradeAttributeSets))
	for _, set := range m.UpgradeAttributeSets {
		ok := true
		for _, model := range set.Models {
			if _, found := valid[model.DocumentID]; !found {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		for _, pattern := range set.Patterns {
			if _, found := valid[pattern.DocumentID]; !found {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		sets = append(sets, set)
	}
	m.UpgradeAttributeSets = sets
	m.UpgradeAttributeSetCount = len(sets)
	m.TotalBytes = total

	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*path, append(out, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("cleaned %s: documents %d -> %d, gifts -> %d, collectible_sets -> %d\n",
		filepath.Clean(*path), beforeDocs, len(docs), len(gifts), len(sets))
}
