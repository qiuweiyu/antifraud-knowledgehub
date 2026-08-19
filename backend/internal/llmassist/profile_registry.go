package llmassist

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxAssistedProfiles             = 16
	maxProfileIDBytes               = 64
	maxProfileDisplayNameBytes      = 120
	maxProviderDisplayNameBytes     = 80
	maxModelDisplayNameBytes        = 160
	maxProfileDisclosureBytes       = 512
)

type ProfileAvailability string

const (
	ProfileAvailabilityAvailable   ProfileAvailability = "available"
	ProfileAvailabilityUnavailable ProfileAvailability = "unavailable"
	ProfileAvailabilityDisabled    ProfileAvailability = "disabled"
)

// ProfilePublicMetadata is the only profile representation intended for
// presentation outside the trusted server runtime. It deliberately contains no
// credential, endpoint, request-template, system-prompt or provider options.
type ProfilePublicMetadata struct {
	ID                  string              `json:"id"`
	DisplayName         string              `json:"display_name"`
	ProviderDisplayName string              `json:"provider_display_name"`
	ModelDisplayName    string              `json:"model_display_name"`
	Availability        ProfileAvailability `json:"availability"`
	Disclosure          string              `json:"disclosure"`
}

// ProfileDefinition is server-only construction input. APIKey is consumed only
// while the provider is constructed and is never retained by ProfileRegistry.
type ProfileDefinition struct {
	ID                  string
	DisplayName         string
	Provider            string
	ProviderDisplayName string
	Model               string
	ModelDisplayName    string
	APIKey              string
	Timeout             time.Duration
	Enabled             bool
	Disclosure          string
}

// ResolvedProfile is a trusted-server runtime value. It contains the approved
// provider/model routing decision and a bounded assistance service, but no
// provider credential.
type ResolvedProfile struct {
	ID       string
	Provider string
	Model    string
	Service  Service
	Public   ProfilePublicMetadata
}

type profileEntry struct {
	public   ProfilePublicMetadata
	provider string
	model    string
	service  Service
	enabled  bool
}

type ProfileRegistry struct {
	profiles map[string]profileEntry
	public   []ProfilePublicMetadata
}

func NewProfileRegistry(providerRegistry *Registry, definitions []ProfileDefinition) (*ProfileRegistry, error) {
	if providerRegistry == nil {
		return nil, errors.New("LLM provider registry is required")
	}
	if len(definitions) > maxAssistedProfiles {
		return nil, errors.New("assisted AI profile count exceeds limit")
	}

	registry := &ProfileRegistry{
		profiles: make(map[string]profileEntry, len(definitions)),
		public:   make([]ProfilePublicMetadata, 0, len(definitions)),
	}

	for _, definition := range definitions {
		id, err := normalizeProfileID(definition.ID)
		if err != nil {
			return nil, err
		}
		if _, exists := registry.profiles[id]; exists {
			return nil, errors.New("assisted AI profile is already registered")
		}

		displayName, err := normalizeProfileText(definition.DisplayName, maxProfileDisplayNameBytes, "assisted AI profile display name")
		if err != nil {
			return nil, err
		}
		providerDisplayName, err := normalizeProfileText(definition.ProviderDisplayName, maxProviderDisplayNameBytes, "assisted AI provider display name")
		if err != nil {
			return nil, err
		}
		modelDisplayName, err := normalizeProfileText(definition.ModelDisplayName, maxModelDisplayNameBytes, "assisted AI model display name")
		if err != nil {
			return nil, err
		}
		disclosure, err := normalizeProfileText(definition.Disclosure, maxProfileDisclosureBytes, "assisted AI profile disclosure")
		if err != nil {
			return nil, err
		}
		providerName, err := normalizeProviderName(definition.Provider)
		if err != nil {
			return nil, err
		}
		model, err := NormalizeModelIdentifier(definition.Model)
		if err != nil {
			return nil, err
		}

		availability := ProfileAvailabilityDisabled
		var service Service
		if definition.Enabled {
			provider, err := providerRegistry.Create(providerName, ProviderConfig{
				Model:  model,
				APIKey: definition.APIKey,
			})
			if err != nil {
				return nil, errors.New("assisted AI profile provider configuration is invalid")
			}
			service, err = NewService(provider, definition.Timeout)
			if err != nil {
				return nil, errors.New("assisted AI profile service configuration is invalid")
			}
			availability = ProfileAvailabilityAvailable
		}

		public := ProfilePublicMetadata{
			ID:                  id,
			DisplayName:         displayName,
			ProviderDisplayName: providerDisplayName,
			ModelDisplayName:    modelDisplayName,
			Availability:        availability,
			Disclosure:          disclosure,
		}
		registry.profiles[id] = profileEntry{
			public:   public,
			provider: providerName,
			model:    model,
			service:  service,
			enabled:  definition.Enabled,
		}
		registry.public = append(registry.public, public)
	}

	return registry, nil
}

func (r *ProfileRegistry) Resolve(id string) (ResolvedProfile, error) {
	if r == nil {
		return ResolvedProfile{}, errors.New("assisted AI profile registry is required")
	}
	profileID, err := normalizeProfileID(id)
	if err != nil {
		return ResolvedProfile{}, err
	}
	entry, exists := r.profiles[profileID]
	if !exists {
		return ResolvedProfile{}, errors.New("assisted AI profile is not registered")
	}
	if !entry.enabled || entry.public.Availability != ProfileAvailabilityAvailable {
		return ResolvedProfile{}, errors.New("assisted AI profile is disabled")
	}
	return ResolvedProfile{
		ID:       profileID,
		Provider: entry.provider,
		Model:    entry.model,
		Service:  entry.service,
		Public:   entry.public,
	}, nil
}

func (r *ProfileRegistry) PublicProfiles() []ProfilePublicMetadata {
	if r == nil || len(r.public) == 0 {
		return nil
	}
	return append([]ProfilePublicMetadata(nil), r.public...)
}

func normalizeProfileID(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New("assisted AI profile ID must be valid UTF-8")
	}
	if value == "" || strings.TrimSpace(value) != value {
		return "", errors.New("assisted AI profile ID is required without surrounding whitespace")
	}
	if len([]byte(value)) > maxProfileIDBytes {
		return "", errors.New("assisted AI profile ID exceeds limit")
	}
	if value != strings.ToLower(value) {
		return "", errors.New("assisted AI profile ID must be lowercase")
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || (i > 0 && (c == '-' || c == '_')) {
			continue
		}
		return "", errors.New("assisted AI profile ID contains unsupported characters")
	}
	return value, nil
}

func normalizeProfileText(value string, maxBytes int, field string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New(field + " must be valid UTF-8")
	}
	text := strings.TrimSpace(value)
	if text == "" {
		return "", errors.New(field + " is required")
	}
	if len([]byte(text)) > maxBytes {
		return "", errors.New(field + " exceeds limit")
	}
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			return "", errors.New(field + " contains control characters")
		}
	}
	return text, nil
}
