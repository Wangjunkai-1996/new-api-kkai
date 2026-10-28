package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var ErrShopSecretUnavailable = errors.New("SHOP_SECRET_UNAVAILABLE")

// ShopSecretBinding is stable across encryption-key rotation. Never bind an
// encryption envelope to a mutable order version or a display label.
type ShopSecretBinding struct {
	Purpose         string `json:"purpose"`
	TenantID        int64  `json:"tenant_id"`
	ObjectID        string `json:"object_id"`
	RelatedID       string `json:"related_id,omitempty"`
	TemplateVersion string `json:"template_version"`
}

type ShopFingerprint struct {
	KeyVersion string
	DomainHash string
	Digest     string
}

// Keys are explicit, independent 256-bit keyrings; no session/CRYPTO secret
// fallback is allowed. Keeping old entries permits decrypting retained data.
func shopKeyring(purpose string) (string, map[string][]byte, error) {
	prefix := "SHOP_SECRET_"
	if purpose == "fingerprint" {
		prefix = "SHOP_FINGERPRINT_"
	}
	active, raw := os.Getenv(prefix+"ACTIVE_KEY_ID"), os.Getenv(prefix+"KEYS")
	if path := strings.TrimSpace(os.Getenv(prefix + "KEYS_FILE")); path != "" {
		file, err := os.Open(path)
		if err != nil {
			return "", nil, ErrShopSecretUnavailable
		}
		defer file.Close()
		content, err := io.ReadAll(io.LimitReader(file, 8193))
		if err != nil || len(content) > 8192 {
			return "", nil, ErrShopSecretUnavailable
		}
		raw = string(content)
	}
	if !shopKeyID(active) || len(raw) == 0 || len(raw) > 8192 {
		return "", nil, ErrShopSecretUnavailable
	}
	var encoded map[string]string
	if common.UnmarshalStrict([]byte(raw), &encoded) != nil || len(encoded) == 0 || len(encoded) > 16 {
		return "", nil, ErrShopSecretUnavailable
	}
	keys := make(map[string][]byte, len(encoded))
	for id, value := range encoded {
		key, err := base64.StdEncoding.Strict().DecodeString(value)
		if !shopKeyID(id) || err != nil || len(key) != 32 {
			return "", nil, ErrShopSecretUnavailable
		}
		keys[id] = key
	}
	if keys[active] == nil {
		return "", nil, ErrShopSecretUnavailable
	}
	return active, keys, nil
}

func shopKeyID(id string) bool {
	if len(id) == 0 || len(id) > 32 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func ShopSecretsReady() bool {
	_, encryption, err := shopKeyring("encryption")
	if err != nil {
		return false
	}
	_, fingerprints, err := shopKeyring("fingerprint")
	if err != nil {
		return false
	}
	for _, a := range encryption {
		for _, b := range fingerprints {
			if hmac.Equal(a, b) {
				return false
			}
		}
	}
	return true
}

func shopSecretAAD(binding ShopSecretBinding) ([]byte, error) {
	if binding.TenantID != model.PlatformTenantID || !shopProofID(binding.ObjectID) || !shopProofID(binding.TemplateVersion) || (binding.RelatedID != "" && !shopProofID(binding.RelatedID)) {
		return nil, ErrShopSecretUnavailable
	}
	switch binding.Purpose {
	case "inventory", "delivery", "buyer_input", "payment_snapshot", "checkout":
	default:
		return nil, ErrShopSecretUnavailable
	}
	return common.Marshal([]any{"kkai-shop-secret-v1", binding})
}

func SealShopSecret(value any, binding ShopSecretBinding) (model.ShopSecret, error) {
	if !ShopSecretsReady() {
		return model.ShopSecret{}, ErrShopSecretUnavailable
	}
	version, keys, err := shopKeyring("encryption")
	if err != nil {
		return model.ShopSecret{}, err
	}
	aad, err := shopSecretAAD(binding)
	if err != nil {
		return model.ShopSecret{}, err
	}
	plain, err := common.Marshal(value)
	if err != nil || len(plain) == 0 || len(plain) > 32<<10 {
		return model.ShopSecret{}, ErrShopSecretUnavailable
	}
	block, err := aes.NewCipher(keys[version])
	if err != nil {
		return model.ShopSecret{}, ErrShopSecretUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return model.ShopSecret{}, ErrShopSecretUnavailable
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return model.ShopSecret{}, ErrShopSecretUnavailable
	}
	blob := aead.Seal(nonce, nonce, plain, aad)
	return model.ShopSecret{KeyVersion: version, Ciphertext: "v1." + version + "." + base64.RawURLEncoding.EncodeToString(blob)}, nil
}

func OpenShopSecret(secret model.ShopSecret, binding ShopSecretBinding, value any) error {
	if len(secret.Ciphertext) > 48<<10 {
		return ErrShopSecretUnavailable
	}
	parts := strings.Split(secret.Ciphertext, ".")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] != secret.KeyVersion {
		return ErrShopSecretUnavailable
	}
	_, keys, err := shopKeyring("encryption")
	if err != nil || keys[secret.KeyVersion] == nil {
		return ErrShopSecretUnavailable
	}
	aad, err := shopSecretAAD(binding)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(keys[secret.KeyVersion])
	if err != nil {
		return ErrShopSecretUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return ErrShopSecretUnavailable
	}
	blob, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(blob) < aead.NonceSize()+aead.Overhead() {
		return ErrShopSecretUnavailable
	}
	plain, err := aead.Open(nil, blob[:aead.NonceSize()], blob[aead.NonceSize():], aad)
	if err != nil || common.Unmarshal(plain, value) != nil {
		return ErrShopSecretUnavailable
	}
	return nil
}

// ShopIdentityFingerprints includes every retained fingerprint key so a
// tombstone encrypted under a previous key cannot be imported as fresh stock.
// Identity normalization belongs to the product template, never to this helper.
func ShopIdentityFingerprints(domain, identity string) ([]ShopFingerprint, error) {
	active, keys, err := shopKeyring("fingerprint")
	if err != nil || domain == "" || len(domain) > 500 || identity == "" || len(identity) > 4096 {
		return nil, ErrShopSecretUnavailable
	}
	domainBytes, _ := common.Marshal([]string{"kkai-shop-identity-domain-v1", domain})
	domainHash := sha256.Sum256(domainBytes)
	versions := make([]string, 0, len(keys))
	for version := range keys {
		if version != active {
			versions = append(versions, version)
		}
	}
	sort.Strings(versions)
	versions = append([]string{active}, versions...)
	result := make([]ShopFingerprint, 0, len(versions))
	payload, _ := common.Marshal([]string{"kkai-shop-stock-identity-v1", domain, identity})
	for _, version := range versions {
		mac := hmac.New(sha256.New, keys[version])
		_, _ = mac.Write(payload)
		result = append(result, ShopFingerprint{KeyVersion: version, DomainHash: hex.EncodeToString(domainHash[:]), Digest: hex.EncodeToString(mac.Sum(nil))})
	}
	return result, nil
}

// Empty keyVersion selects the active key. Replays use the key version stored
// with their original idempotency record, rather than silently changing hashes.
func ShopRequestHash(value any, scope, keyVersion string) (string, string, error) {
	active, keys, err := shopKeyring("fingerprint")
	if err != nil || scope == "" || len(scope) > 256 {
		return "", "", ErrShopSecretUnavailable
	}
	if keyVersion == "" {
		keyVersion = active
	}
	if keys[keyVersion] == nil {
		return "", "", ErrShopSecretUnavailable
	}
	payload, err := common.Marshal([]any{"kkai-shop-request-v1", scope, value})
	if err != nil || len(payload) > (1<<20)+1024 {
		return "", "", ErrShopSecretUnavailable
	}
	mac := hmac.New(sha256.New, keys[keyVersion])
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil)), keyVersion, nil
}
