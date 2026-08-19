package browserauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	maxGrantRecords      = 32
	maxPrincipalIDBytes  = 64
	maxDisplayLabelBytes = 120
	browserGrantPrefix   = "afkhb1_"
	browserGrantEntropy  = 32
)

type GrantDefinition struct {
	PrincipalID         string `json:"principal_id"`
	PrincipalGeneration uint64 `json:"principal_generation"`
	DisplayLabel        string `json:"display_label,omitempty"`
	GrantSHA256         string `json:"grant_sha256"`
	Enabled             bool   `json:"enabled"`
}

type Principal struct {
	ID           string
	Generation   uint64
	DisplayLabel string
}

type grantEntry struct {
	principal Principal
	digest    [sha256.Size]byte
	enabled   bool
}

type GrantRegistry struct {
	entries     []grantEntry
	byPrincipal map[string]grantEntry
}

func ParseGrantRegistryJSON(raw string) (*GrantRegistry, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("browser access grant registry is required")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var definitions []GrantDefinition
	if err := decoder.Decode(&definitions); err != nil {
		return nil, errors.New("browser access grant registry JSON is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("browser access grant registry JSON has trailing data")
	}
	return NewGrantRegistry(definitions)
}

func NewGrantRegistry(definitions []GrantDefinition) (*GrantRegistry, error) {
	if len(definitions) == 0 {
		return nil, errors.New("browser access grant registry must contain at least one record")
	}
	if len(definitions) > maxGrantRecords {
		return nil, errors.New("browser access grant registry exceeds record limit")
	}

	registry := &GrantRegistry{
		entries:     make([]grantEntry, 0, len(definitions)),
		byPrincipal: make(map[string]grantEntry, len(definitions)),
	}
	seenDigests := make(map[[sha256.Size]byte]struct{}, len(definitions))
	for _, definition := range definitions {
		if err := validatePrincipalID(definition.PrincipalID); err != nil {
			return nil, err
		}
		if definition.PrincipalGeneration == 0 || definition.PrincipalGeneration > math.MaxInt32 {
			return nil, errors.New("browser principal generation must be positive and bounded")
		}
		if !utf8.ValidString(definition.DisplayLabel) || len([]byte(definition.DisplayLabel)) > maxDisplayLabelBytes {
			return nil, errors.New("browser principal display label is invalid")
		}
		digestBytes, err := hex.DecodeString(definition.GrantSHA256)
		if err != nil || len(digestBytes) != sha256.Size || len(definition.GrantSHA256) != sha256.Size*2 {
			return nil, errors.New("browser access grant digest must be a 64-character SHA-256 hex value")
		}
		var digest [sha256.Size]byte
		copy(digest[:], digestBytes)
		if _, exists := registry.byPrincipal[definition.PrincipalID]; exists {
			return nil, errors.New("browser principal is duplicated")
		}
		if _, exists := seenDigests[digest]; exists {
			return nil, errors.New("browser access grant digest is duplicated")
		}
		entry := grantEntry{
			principal: Principal{ID: definition.PrincipalID, Generation: definition.PrincipalGeneration, DisplayLabel: definition.DisplayLabel},
			digest:    digest,
			enabled:   definition.Enabled,
		}
		registry.entries = append(registry.entries, entry)
		registry.byPrincipal[definition.PrincipalID] = entry
		seenDigests[digest] = struct{}{}
	}
	return registry, nil
}

func (r *GrantRegistry) Verify(rawGrant string) (Principal, bool) {
	if r == nil || !ValidRawGrant(rawGrant) {
		return Principal{}, false
	}
	digest := sha256.Sum256([]byte(rawGrant))
	for _, entry := range r.entries {
		matched := subtle.ConstantTimeCompare(digest[:], entry.digest[:]) == 1
		if matched && entry.enabled {
			return entry.principal, true
		}
	}
	return Principal{}, false
}

func (r *GrantRegistry) Resolve(principalID string) (Principal, bool) {
	if r == nil {
		return Principal{}, false
	}
	entry, ok := r.byPrincipal[principalID]
	if !ok || !entry.enabled {
		return Principal{}, false
	}
	return entry.principal, true
}

func ValidRawGrant(raw string) bool {
	if !strings.HasPrefix(raw, browserGrantPrefix) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, browserGrantPrefix))
	return err == nil && len(decoded) == browserGrantEntropy
}

func validatePrincipalID(value string) error {
	if !utf8.ValidString(value) || value == "" || len([]byte(value)) > maxPrincipalIDBytes || strings.TrimSpace(value) != value {
		return errors.New("browser principal ID is invalid")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return errors.New("browser principal ID contains unsupported characters")
	}
	return nil
}
