package service

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func shopTestKeys(t *testing.T) {
	t.Helper()
	for purpose, value := range map[string]byte{"SECRET": 1, "FINGERPRINT": 2} {
		raw, err := common.Marshal(map[string]string{"old": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{value}, 32)), "new": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{value + 2}, 32))})
		require.NoError(t, err)
		t.Setenv("SHOP_"+purpose+"_ACTIVE_KEY_ID", "old")
		t.Setenv("SHOP_"+purpose+"_KEYS", string(raw))
		t.Setenv("SHOP_"+purpose+"_KEYS_FILE", "")
	}
}

func TestShopMountedKeyringsOverrideEnvironmentAndFailClosed(t *testing.T) {
	shopTestKeys(t)
	binding := shopObjectBinding("delivery", "sd_mounted", "so_mounted")
	for _, purpose := range []string{"SECRET", "FINGERPRINT"} {
		name := "SHOP_" + purpose + "_KEYS"
		path := filepath.Join(t.TempDir(), "keyring.json")
		require.NoError(t, os.WriteFile(path, []byte(os.Getenv(name)+"\n"), 0o600))
		t.Setenv(name+"_FILE", path)
		t.Setenv(name, "invalid-inline-value")
	}
	require.True(t, ShopSecretsReady())
	secret, err := SealShopSecret(map[string]string{"password": "synthetic"}, binding)
	require.NoError(t, err)
	var value map[string]string
	require.NoError(t, OpenShopSecret(secret, binding, &value))
	assert.Equal(t, "synthetic", value["password"])
	_, _, err = ShopRequestHash("request", "scope", "")
	require.NoError(t, err)
	for _, purpose := range []string{"SECRET", "FINGERPRINT"} {
		t.Run(purpose, func(t *testing.T) {
			name := "SHOP_" + purpose + "_KEYS"
			goodFile := os.Getenv(name + "_FILE")
			good, err := os.ReadFile(goodFile)
			require.NoError(t, err)
			t.Setenv(name, string(good))
			path := filepath.Join(t.TempDir(), "unavailable-keyring.json")
			t.Setenv(name+"_FILE", path)
			for _, content := range []string{"missing", "", strings.Repeat("s", 8193), `{"old":"invalid"}`} {
				if content != "missing" {
					require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
				}
				assert.False(t, ShopSecretsReady(), "invalid mounted keyring must never fall back to inline keys")
				_, err := SealShopSecret("private", binding)
				require.ErrorIs(t, err, ErrShopSecretUnavailable)
				assert.NotContains(t, err.Error(), path)
			}
		})
	}
}

func TestShopSecretBindingAndKeyRotation(t *testing.T) {
	shopTestKeys(t)
	require.True(t, ShopSecretsReady())
	binding := shopObjectBinding("delivery", "sd_one", "so_one")
	secret, err := SealShopSecret(map[string]string{"password": " kept exactly "}, binding)
	require.NoError(t, err)
	hash, key, err := ShopRequestHash(map[string]string{"value": "secret"}, "inventory-import:product", "")
	require.NoError(t, err)
	fingerprints, err := ShopIdentityFingerprints("accounts", "Identity")
	require.NoError(t, err)
	t.Setenv("SHOP_SECRET_ACTIVE_KEY_ID", "new")
	t.Setenv("SHOP_FINGERPRINT_ACTIVE_KEY_ID", "new")
	var output map[string]string
	require.NoError(t, OpenShopSecret(secret, binding, &output))
	assert.Equal(t, " kept exactly ", output["password"])
	replay, replayKey, err := ShopRequestHash(map[string]string{"value": "secret"}, "inventory-import:product", key)
	require.NoError(t, err)
	assert.Equal(t, hash, replay)
	assert.Equal(t, key, replayKey)
	rotated, err := ShopIdentityFingerprints("accounts", "Identity")
	require.NoError(t, err)
	assert.Equal(t, "new", rotated[0].KeyVersion)
	assert.Contains(t, rotated, fingerprints[0], "retained keys must continue finding old tombstones")
	for _, wrong := range []ShopSecretBinding{
		shopObjectBinding("delivery", "sd_other", "so_one"),
		shopObjectBinding("delivery", "sd_one", "so_other"),
		shopObjectBinding("inventory", "sd_one", "so_one"),
		{Purpose: "delivery", TenantID: model.PlatformTenantID, ObjectID: "sd_one", RelatedID: "so_one", TemplateVersion: "2"},
	} {
		assert.ErrorIs(t, OpenShopSecret(secret, wrong, &output), ErrShopSecretUnavailable)
	}
	parts := strings.Split(secret.Ciphertext, ".")
	blob, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	blob[len(blob)-1] ^= 1
	secret.Ciphertext = parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(blob)
	assert.ErrorIs(t, OpenShopSecret(secret, binding, &output), ErrShopSecretUnavailable)
}

func TestShopKeyringRejectsAmbiguousOrSharedSecrets(t *testing.T) {
	shopTestKeys(t)
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	t.Setenv("SHOP_FINGERPRINT_KEYS", `{"old":"`+encoded+`"}`)
	assert.False(t, ShopSecretsReady(), "encryption and fingerprint keys must be independent")
	_, err := SealShopSecret("secret", shopObjectBinding("inventory", "su_one", ""))
	assert.ErrorIs(t, err, ErrShopSecretUnavailable, "imports must reject shared keys even while sales are disabled")
	independent := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	t.Setenv("SHOP_FINGERPRINT_KEYS", `{"old":"`+independent+`"}`)
	t.Setenv("SHOP_SECRET_KEYS", `{"old":"`+encoded+`","old":"`+encoded+`"}`)
	_, err = SealShopSecret("secret", shopObjectBinding("inventory", "su_one", ""))
	assert.ErrorIs(t, err, ErrShopSecretUnavailable)
}

func TestShopDeliveryPayloadPreservesFrozenTemplate(t *testing.T) {
	terms := model.ShopProductTerms{TemplateVersion: 7, DeliveryFields: []model.ShopField{{Key: "account", Label: "Account", Required: true}, {Key: "password", Label: "Password", Required: true}}}
	payload, err := shopDeliveryPayload(terms, []ShopSecretField{{Key: "password", Value: " secret "}, {Key: "account", Value: "user"}})
	require.NoError(t, err)
	assert.Equal(t, []ShopRevealedField{{Key: "account", Label: "Account", Value: "user"}, {Key: "password", Label: "Password", Value: " secret "}}, payload.Fields)
	require.NoError(t, validateShopPayload(payload, terms))
	for _, fields := range [][]ShopSecretField{
		{{Key: "account", Value: "user"}},
		{{Key: "account", Value: "user"}, {Key: "account", Value: "other"}, {Key: "password", Value: "secret"}},
		{{Key: "account", Value: "user"}, {Key: "password", Value: "secret"}, {Key: "extra", Value: "hidden"}},
		{{Key: "account", Value: "user"}, {Key: "password", Value: strings.Repeat("s", 8193)}},
	} {
		_, err := shopDeliveryPayload(terms, fields)
		assert.ErrorIs(t, err, model.ErrShopInvalid)
	}
	payload.Fields[0].Label = "Altered"
	assert.ErrorIs(t, validateShopPayload(payload, terms), model.ErrShopSecret)
	payload.Fields[0].Label = "Account"
	payload.TemplateVersion++
	assert.ErrorIs(t, validateShopPayload(payload, terms), model.ErrShopSecret)
}

func TestShopBuyerInputBoundariesAndMissingRequired(t *testing.T) {
	shopTestKeys(t)
	terms := model.ShopProductTerms{TemplateVersion: 3, BuyerInputSchema: []model.ShopField{{Key: "account_identifier", Required: true}, {Key: "contact_email"}, {Key: "order_note"}}}
	secret, keys, err := EncryptShopBuyerInput("so_one", terms, map[string]string{"contact_email": " user@example.com "})
	require.NoError(t, err, "required fields may be completed after payment")
	assert.Equal(t, []string{"contact_email"}, keys)
	var plain shopBuyerPayload
	require.NoError(t, OpenShopSecret(secret, shopObjectBinding("buyer_input", "so_one", ""), &plain))
	assert.Equal(t, map[string]string{"contact_email": "user@example.com"}, plain.Values)
	for name, values := range map[string]map[string]string{
		"secret field":       {"password": "never request customer passwords"},
		"email display name": {"contact_email": "Someone <user@example.com>"},
		"email headers":      {"contact_email": "user@example.com\r\nBcc: other@example.com"},
		"long account":       {"account_identifier": strings.Repeat("x", 321)},
		"long note":          {"order_note": strings.Repeat("x", 2001)},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := EncryptShopBuyerInput("so_one", terms, values)
			assert.ErrorIs(t, err, model.ErrShopInvalid)
		})
	}
}

func TestShopProofRejectsAmbiguousContext(t *testing.T) {
	valid := `{"action":"reveal","target_id":"su_one","expected_version":"1","details":{"reason":"inventory review"}}`
	_, err := normalizeShopVerification(VerificationOperation{Scope: VerificationScopeShopInventoryReveal, Context: []byte(valid)})
	require.NoError(t, err)
	for name, raw := range map[string]string{
		"duplicate":            strings.Replace(valid, `"action":"reveal"`, `"action":"reveal","action":"reveal"`, 1),
		"unexpected secret":    strings.Replace(valid, `"reason":"inventory review"`, `"reason":"inventory review","secret_fields":[]`, 1),
		"null":                 strings.Replace(valid, `"reason":"inventory review"`, `"reason":null`, 1),
		"empty":                strings.Replace(valid, `"reason":"inventory review"`, `"reason":" "`, 1),
		"noncanonical version": strings.Replace(valid, `"expected_version":"1"`, `"expected_version":"01"`, 1),
		"duplicate details":    strings.Replace(valid, `"reason":"inventory review"`, `"reason":"inventory review","reason":"changed"`, 1),
		"wrong action":         strings.Replace(valid, `"action":"reveal"`, `"action":"approve"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := normalizeShopVerification(VerificationOperation{Scope: VerificationScopeShopInventoryReveal, Context: []byte(raw)})
			assert.ErrorIs(t, err, ErrVerificationContextInvalid)
		})
	}
	_, err = normalizeShopVerification(VerificationOperation{Scope: VerificationScopeShopDeliveryReveal, Context: []byte(valid)})
	assert.ErrorIs(t, err, ErrVerificationContextInvalid, "inventory proof cannot authorize a delivery reveal")
}

func TestShopProofSupportsRefundEvidenceConclusions(t *testing.T) {
	proof := ShopVerificationContext{Action: "create", TargetID: "sr_one", ExpectedVersion: "1", IdempotencyKey: "00000000-0000-4000-8000-000000000001", Details: ShopVerificationDetails{Source: "merchant_console", ProviderTransactionID: "provider-trade", Money: &ShopProofMoney{AmountMinor: "100", Currency: "CNY", Exponent: 2}, ObservedAt: "2026-01-01T00:00:00Z", ReferenceDigest: strings.Repeat("a", 64), SummaryDigest: strings.Repeat("b", 64)}}
	for _, state := range []string{"not_refunded", "refund_failed", "not_executed", "refund_succeeded", "paid", "unknown"} {
		proof.Details.ObservedStatus = state
		raw, err := common.Marshal(proof)
		require.NoError(t, err)
		parsed, err := normalizeShopVerification(VerificationOperation{Scope: VerificationScopeShopEvidenceCreate, Context: raw})
		require.NoError(t, err, state)
		assert.Equal(t, state, parsed.Details.ObservedStatus)
	}
	proof.Details.ObservedStatus = "invented_success"
	raw, err := common.Marshal(proof)
	require.NoError(t, err)
	_, err = normalizeShopVerification(VerificationOperation{Scope: VerificationScopeShopEvidenceCreate, Context: raw})
	assert.ErrorIs(t, err, ErrVerificationContextInvalid)
	proof.Details.ObservedStatus, proof.Details.Money.AmountMinor = "not_refunded", "0"
	raw, err = common.Marshal(proof)
	require.NoError(t, err)
	_, err = normalizeShopVerification(VerificationOperation{Scope: VerificationScopeShopEvidenceCreate, Context: raw})
	assert.NoError(t, err, "evidence may describe a zero observation without authorizing a zero refund")
}

func TestShopRefundRecordProofAllowsNoPublicNote(t *testing.T) {
	proof := ShopVerificationContext{Action: "record_attempt", TargetID: "sf_one", ExpectedVersion: "1", IdempotencyKey: "00000000-0000-4000-8000-000000000001", Details: ShopVerificationDetails{AttemptID: "sfa_one", OperatedAt: "2026-01-01T00:00:00Z", EvidenceIDs: []string{"se_two", "se_one", "se_two"}}}
	raw, err := common.Marshal(proof)
	require.NoError(t, err)
	parsed, err := normalizeShopVerification(VerificationOperation{Scope: VerificationScopeShopRefundManage, Context: raw})
	require.NoError(t, err)
	assert.Empty(t, parsed.Details.PublicNote)
	assert.Equal(t, []string{"se_one", "se_two"}, parsed.Details.EvidenceIDs)
}

func TestShopDeliveryProofBindsReplacementCase(t *testing.T) {
	proof := ShopVerificationContext{Action: "first", TargetID: "so_one", ExpectedVersion: "1", IdempotencyKey: "00000000-0000-4000-8000-000000000001", Details: ShopVerificationDetails{Reason: "operator delivery", TemplateVersion: "2"}}
	raw, err := common.Marshal(proof)
	require.NoError(t, err)
	_, err = normalizeShopVerification(VerificationOperation{Scope: VerificationScopeShopDeliveryCreate, Context: raw})
	require.NoError(t, err)
	proof.Action = "replacement"
	raw, err = common.Marshal(proof)
	require.NoError(t, err)
	_, err = normalizeShopVerification(VerificationOperation{Scope: VerificationScopeShopDeliveryCreate, Context: raw})
	assert.ErrorIs(t, err, ErrVerificationContextInvalid, "a replacement must bind its after-sales case and version")
	proof.Details.CaseID, proof.Details.CaseVersion = "sa_one", "3"
	raw, err = common.Marshal(proof)
	require.NoError(t, err)
	_, err = normalizeShopVerification(VerificationOperation{Scope: VerificationScopeShopDeliveryCreate, Context: raw})
	assert.NoError(t, err)
}
