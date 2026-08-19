package browserauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	SessionTTL         = time.Hour
	sessionTokenPrefix = "afkhs1_"
	sessionEntropy     = 32
	sessionKeyDomain   = "afkh-browser-session-key-v1\x00"
	csrfDomain         = "afkh-browser-csrf-v1\x00"
)

var (
	ErrInvalidSession = errors.New("browser session is invalid or expired")
	ErrRevokedSession = errors.New("browser session principal was revoked")
)

type SessionRecord struct {
	PrincipalID         string `json:"principal_id"`
	PrincipalGeneration uint64 `json:"principal_generation"`
	IssuedUnix          int64  `json:"issued_unix"`
	ExpiresUnix         int64  `json:"expires_unix"`
}

type SessionStore struct {
	Client *redis.Client
	Random io.Reader
	Now    func() time.Time
}

func (s SessionStore) Create(ctx context.Context, principal Principal) (string, string, SessionRecord, error) {
	if s.Client == nil {
		return "", "", SessionRecord{}, errors.New("browser session Redis client is not configured")
	}
	if principal.ID == "" || principal.Generation == 0 {
		return "", "", SessionRecord{}, errors.New("browser session principal is invalid")
	}
	random := s.Random
	if random == nil {
		random = rand.Reader
	}
	entropy := make([]byte, sessionEntropy)
	if _, err := io.ReadFull(random, entropy); err != nil {
		return "", "", SessionRecord{}, errors.New("browser session token generation failed")
	}
	rawToken := sessionTokenPrefix + base64.RawURLEncoding.EncodeToString(entropy)
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	record := SessionRecord{
		PrincipalID:         principal.ID,
		PrincipalGeneration: principal.Generation,
		IssuedUnix:          now.Unix(),
		ExpiresUnix:         now.Add(SessionTTL).Unix(),
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return "", "", SessionRecord{}, errors.New("browser session encoding failed")
	}
	created, err := s.Client.SetNX(ctx, sessionRedisKey(rawToken), payload, SessionTTL).Result()
	if err != nil {
		return "", "", SessionRecord{}, err
	}
	if !created {
		return "", "", SessionRecord{}, errors.New("browser session token collision")
	}
	return rawToken, CSRFToken(rawToken), record, nil
}

func (s SessionStore) Validate(ctx context.Context, rawToken string, registry *GrantRegistry) (SessionRecord, Principal, error) {
	if s.Client == nil {
		return SessionRecord{}, Principal{}, errors.New("browser session Redis client is not configured")
	}
	if registry == nil || !validSessionToken(rawToken) {
		return SessionRecord{}, Principal{}, ErrInvalidSession
	}
	payload, err := s.Client.Get(ctx, sessionRedisKey(rawToken)).Bytes()
	if errors.Is(err, redis.Nil) {
		return SessionRecord{}, Principal{}, ErrInvalidSession
	}
	if err != nil {
		return SessionRecord{}, Principal{}, err
	}
	var record SessionRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return SessionRecord{}, Principal{}, errors.New("browser session state is invalid")
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	if record.PrincipalID == "" || record.PrincipalGeneration == 0 || record.ExpiresUnix <= now.Unix() || record.IssuedUnix > record.ExpiresUnix {
		return SessionRecord{}, Principal{}, ErrInvalidSession
	}
	principal, ok := registry.Resolve(record.PrincipalID)
	if !ok || principal.Generation != record.PrincipalGeneration {
		return SessionRecord{}, Principal{}, ErrRevokedSession
	}
	return record, principal, nil
}

func (s SessionStore) Delete(ctx context.Context, rawToken string) error {
	if s.Client == nil {
		return errors.New("browser session Redis client is not configured")
	}
	if !validSessionToken(rawToken) {
		return ErrInvalidSession
	}
	return s.Client.Del(ctx, sessionRedisKey(rawToken)).Err()
}

func CSRFToken(rawSessionToken string) string {
	digest := sha256.Sum256(append([]byte(csrfDomain), []byte(rawSessionToken)...))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func ValidCSRF(rawSessionToken, presented string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(presented)
	if err != nil || len(decoded) != sha256.Size {
		return false
	}
	expected := sha256.Sum256(append([]byte(csrfDomain), []byte(rawSessionToken)...))
	return subtle.ConstantTimeCompare(expected[:], decoded) == 1
}

func sessionRedisKey(rawSessionToken string) string {
	digest := sha256.Sum256(append([]byte(sessionKeyDomain), []byte(rawSessionToken)...))
	return "afkh:browser-session:v1:" + hex.EncodeToString(digest[:])
}

func validSessionToken(raw string) bool {
	if len(raw) <= len(sessionTokenPrefix) || raw[:len(sessionTokenPrefix)] != sessionTokenPrefix {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw[len(sessionTokenPrefix):])
	return err == nil && len(decoded) == sessionEntropy
}
