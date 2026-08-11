package domain

import (
	"errors"
	"time"
)

var (
	ErrGifCatalogUnavailable   = errors.New("gif catalog is not configured")
	ErrGifCatalogFileInvalid   = errors.New("gif catalog file invalid")
	ErrGifCatalogEntryInvalid  = errors.New("gif catalog entry invalid")
	ErrGifCatalogEntryNotFound = errors.New("gif catalog entry not found")
	ErrGifCatalogFull          = errors.New("gif catalog is full")
	ErrGifCatalogSourceChanged = errors.New("gif catalog seed source changed")
)

const (
	MaxGifCatalogTitleLen     = 128
	MaxGifCatalogEntries      = MaxBotInlineResults
	MaxGifCatalogUploadSize   = 50 << 20
	MaxGifCatalogDocumentSize = 200 << 20
)

// GifCatalogEntry is one operator-curated GIFV document served by @gif.
// SourceFilename/SourceSHA256 are startup-seed identity, not display fields.
type GifCatalogEntry struct {
	ID             int64     `json:"ID,string"`
	Title          string    `json:"Title"`
	DocumentID     int64     `json:"DocumentID,string"`
	Enabled        bool      `json:"Enabled"`
	SortOrder      int       `json:"SortOrder"`
	CreatedBy      string    `json:"CreatedBy"`
	SourceFilename string    `json:"SourceFilename"`
	SourceSHA256   string    `json:"SourceSHA256"`
	CreatedAt      time.Time `json:"CreatedAt"`
	UpdatedAt      time.Time `json:"UpdatedAt"`
}
