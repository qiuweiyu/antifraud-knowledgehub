package browserauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func testRawGrant(seed byte) string {
	return browserGrantPrefix + base64.RawURLEncoding.EncodeToString(bytesRepeat(seed, browserGrantEntropy))
}

func testGrantDefinition(id string, generation uint64, raw string, enabled bool) GrantDefinition {
	digest := sha256.Sum256([]byte(raw))
	return GrantDefinition{
		PrincipalID:         id,
		PrincipalGeneration: generation,
		DisplayLabel:        "Beta user",
		GrantSHA256:         hex.EncodeToString(digest[:]),
		Enabled:             enabled,
	}
}

func bytesRepeat(value byte, count int) []byte {
	out := make([]byte, count)
	for i := range out {
		out[i] = value
	}
	return out
}

func TestGrantRegistryVerifiesOnlyEnabledDigestBackedGrant(t *testing.T) {
	raw := testRawGrant(1)
	disabledRaw := testRawGrant(2)
	registry, err := NewGrantRegistry([]GrantDefinition{
		testGrantDefinition("beta-user", 3, raw, true),
		testGrantDefinition("disabled-user", 1, disabledRaw, false),
	})
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	principal, ok := registry.Verify(raw)
	if !ok || principal.ID != "beta-user" || principal.Generation != 3 {
		t.Fatalf("unexpected verified principal: %+v ok=%v", principal, ok)
	}
	if _, ok := registry.Verify(disabledRaw); ok {
		t.Fatal("disabled grant must not verify")
	}
	if _, ok := registry.Verify("not-a-grant"); ok {
		t.Fatal("malformed grant must not verify")
	}
	if strings.Contains(principal.DisplayLabel, raw) {
		t.Fatal("principal metadata must not contain raw grant")
	}
}

func TestGrantRegistryRejectsDuplicateAndInvalidGeneration(t *testing.T) {
	raw := testRawGrant(3)
	definition := testGrantDefinition("beta-user", 1, raw, true)
	duplicate := testGrantDefinition("beta-user-2", 1, raw, true)
	if _, err := NewGrantRegistry([]GrantDefinition{definition, duplicate}); err == nil {
		t.Fatal("duplicate grant digest must fail")
	}
	definition.PrincipalGeneration = 0
	if _, err := NewGrantRegistry([]GrantDefinition{definition}); err == nil {
		t.Fatal("zero generation must fail")
	}
}

func TestParseGrantRegistryJSONRejectsUnknownFields(t *testing.T) {
	if _, err := ParseGrantRegistryJSON(`[{"principal_id":"p","principal_generation":1,"grant_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","enabled":true,"raw_grant":"secret"}]`); err == nil {
		t.Fatal("unknown grant registry fields must fail")
	}
}

func TestCanonicalOriginAndTrustedProxySource(t *testing.T) {
	if _, err := ValidateCanonicalOrigin("http://example.com", true); err == nil {
		t.Fatal("production HTTP origin must fail")
	}
	if got, err := ValidateCanonicalOrigin("http://127.0.0.1:5173", false); err != nil || got != "http://127.0.0.1:5173" {
		t.Fatalf("loopback origin got=%q err=%v", got, err)
	}
	trusted, err := ParseTrustedProxyCIDRs([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("trusted proxies: %v", err)
	}
	source, err := ClientSource("203.0.113.10:4567", "198.51.100.1", trusted)
	if err != nil || source != "203.0.113.10" {
		t.Fatalf("untrusted peer must ignore XFF: source=%q err=%v", source, err)
	}
	source, err = ClientSource("10.0.0.5:4567", "198.51.100.9, 10.0.0.4", trusted)
	if err != nil || source != "198.51.100.9" {
		t.Fatalf("trusted chain source=%q err=%v", source, err)
	}
	if _, err := ClientSource("10.0.0.5:4567", "attacker-value", trusted); err == nil {
		t.Fatal("malformed trusted XFF must fail closed")
	}
}
