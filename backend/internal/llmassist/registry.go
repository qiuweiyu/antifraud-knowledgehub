package llmassist

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	ProviderOpenAI          = "openai"
	ProviderGemini          = "gemini"
	maxProviderNameBytes    = 32
	maxModelIdentifierBytes = 128
)

// ProviderConfig contains only the provider-construction values shared by the
// registry boundary. Provider adapters own protocol-specific configuration.
type ProviderConfig struct {
	Model  string
	APIKey string
}

type Factory func(ProviderConfig) (Provider, error)

type Registry struct {
	factories map[string]Factory
}

func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

func NewDefaultRegistry() (*Registry, error) {
	registry := NewRegistry()
	if err := registry.Register(ProviderOpenAI, func(cfg ProviderConfig) (Provider, error) {
		return NewOpenAIProvider(cfg.APIKey, cfg.Model)
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(ProviderGemini, func(cfg ProviderConfig) (Provider, error) {
		return NewGeminiProvider(cfg.APIKey, cfg.Model)
	}); err != nil {
		return nil, err
	}
	return registry, nil
}

func (r *Registry) Register(name string, factory Factory) error {
	if r == nil {
		return errors.New("LLM provider registry is required")
	}
	providerName, err := normalizeProviderName(name)
	if err != nil {
		return err
	}
	if factory == nil {
		return errors.New("LLM provider factory is required")
	}
	if r.factories == nil {
		r.factories = make(map[string]Factory)
	}
	if _, exists := r.factories[providerName]; exists {
		return errors.New("LLM provider is already registered")
	}
	r.factories[providerName] = factory
	return nil
}

func (r *Registry) Create(name string, cfg ProviderConfig) (Provider, error) {
	if r == nil {
		return nil, errors.New("LLM provider registry is required")
	}
	providerName, err := normalizeProviderName(name)
	if err != nil {
		return nil, err
	}
	factory, exists := r.factories[providerName]
	if !exists {
		return nil, errors.New("LLM provider is not registered")
	}
	model, err := NormalizeModelIdentifier(cfg.Model)
	if err != nil {
		return nil, err
	}
	cfg.Model = model
	return factory(cfg)
}

func (r *Registry) Has(name string) bool {
	if r == nil {
		return false
	}
	providerName, err := normalizeProviderName(name)
	if err != nil {
		return false
	}
	_, exists := r.factories[providerName]
	return exists
}

func NormalizeModelIdentifier(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New("LLM model identifier must be valid UTF-8")
	}
	model := strings.TrimSpace(value)
	if model == "" {
		return "", errors.New("LLM model identifier is required")
	}
	if len([]byte(model)) > maxModelIdentifierBytes {
		return "", errors.New("LLM model identifier exceeds limit")
	}
	for i := 0; i < len(model); i++ {
		if model[i] < 0x20 || model[i] == 0x7f {
			return "", errors.New("LLM model identifier contains control characters")
		}
	}
	return model, nil
}

func normalizeProviderName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", errors.New("LLM provider name is required")
	}
	if len(name) > maxProviderNameBytes {
		return "", errors.New("LLM provider name exceeds limit")
	}
	if name != strings.ToLower(name) {
		return "", errors.New("LLM provider name must be lowercase")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return "", errors.New("LLM provider name contains unsupported characters")
	}
	return name, nil
}
