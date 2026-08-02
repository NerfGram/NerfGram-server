// Package loadharness implements the real-MTProto capacity harness used by
// cmd/telesrv-load. It deliberately uses the published gotd fork instead of
// server-internal handlers or direct database fixtures.
package loadharness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ManifestVersion = 1

// Endpoint is the immutable wire target shared by provisioning and runs.
type Endpoint struct {
	Address    string `json:"address"`
	DC         int    `json:"dc"`
	APIID      int    `json:"api_id"`
	APIHash    string `json:"api_hash"`
	RSAKeyPath string `json:"rsa_key_path"`
	Obfuscated bool   `json:"obfuscated"`
	PFS        bool   `json:"pfs"`
	TempKeyTTL int    `json:"temp_key_ttl_seconds,omitempty"`
}

// SessionRecord maps one physical MTProto session file to one logical account.
// It contains routing facts only; auth key material remains in encrypted files.
type SessionRecord struct {
	Index        int    `json:"index"`
	AccountIndex int    `json:"account_index"`
	DeviceIndex  int    `json:"device_index"`
	Phone        string `json:"phone"`
	FirstName    string `json:"first_name"`
	SessionFile  string `json:"session_file"`
	UserID       int64  `json:"user_id"`
	AccessHash   int64  `json:"access_hash"`
}

// Manifest never embeds session encryption keys, auth keys, phone-code hashes
// or raw server errors. It does contain generated test phone/user routing data,
// so it remains a controlled run artifact and is not copied into RunReport.
type Manifest struct {
	Version   int             `json:"version"`
	CreatedAt time.Time       `json:"created_at"`
	Endpoint  Endpoint        `json:"endpoint"`
	Sessions  []SessionRecord `json:"sessions"`
}

func (e Endpoint) Validate() error {
	if strings.TrimSpace(e.Address) == "" {
		return errors.New("endpoint address is required")
	}
	if e.DC == 0 {
		return errors.New("endpoint DC must be non-zero")
	}
	if e.APIID <= 0 || strings.TrimSpace(e.APIHash) == "" {
		return errors.New("endpoint api_id and api_hash are required")
	}
	if strings.TrimSpace(e.RSAKeyPath) == "" {
		return errors.New("endpoint RSA key path is required")
	}
	return nil
}

func (m *Manifest) Validate() error {
	if m == nil {
		return errors.New("nil manifest")
	}
	if m.Version != ManifestVersion {
		return fmt.Errorf("manifest version %d, want %d", m.Version, ManifestVersion)
	}
	if err := m.Endpoint.Validate(); err != nil {
		return err
	}
	indices := make(map[int]struct{}, len(m.Sessions))
	files := make(map[string]struct{}, len(m.Sessions))
	for _, session := range m.Sessions {
		if session.Index < 0 || session.AccountIndex < 0 || session.DeviceIndex < 0 {
			return fmt.Errorf("session %d has a negative index", session.Index)
		}
		if _, ok := indices[session.Index]; ok {
			return fmt.Errorf("duplicate session index %d", session.Index)
		}
		indices[session.Index] = struct{}{}
		if strings.TrimSpace(session.Phone) == "" || strings.TrimSpace(session.SessionFile) == "" {
			return fmt.Errorf("session %d is missing phone or session_file", session.Index)
		}
		clean := filepath.Clean(session.SessionFile)
		if filepath.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
			return fmt.Errorf("session %d has unsafe session_file %q", session.Index, session.SessionFile)
		}
		if _, ok := files[clean]; ok {
			return fmt.Errorf("duplicate session file %q", clean)
		}
		files[clean] = struct{}{}
		if session.UserID <= 0 {
			return fmt.Errorf("session %d has no provisioned user_id", session.Index)
		}
	}
	return nil
}

func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	sort.Slice(manifest.Sessions, func(i, j int) bool { return manifest.Sessions[i].Index < manifest.Sessions[j].Index })
	return &manifest, nil
}

func WriteManifest(path string, manifest *Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, 0o600)
}

func resolveSessionPath(manifestPath string, record SessionRecord) string {
	return filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(record.SessionFile))
}
