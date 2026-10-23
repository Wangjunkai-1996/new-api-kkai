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
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var ErrWalletSecretUnavailable = errors.New("wallet payment encryption key unavailable")

// An explicit stable secret is required: the random session default must never
// encrypt a payable session that becomes unreadable after an application restart.
func walletPaymentKey() ([]byte, string, error) {
	secret := os.Getenv("WALLET_PAYMENT_SECRET")
	if secret == "" {
		secret = os.Getenv("CRYPTO_SECRET")
	}
	if len(secret) < 32 {
		return nil, "", ErrWalletSecretUnavailable
	}
	key := sha256.Sum256([]byte("wallet-payment-v1\x00" + secret))
	version := sha256.Sum256(key[:])
	return key[:], hex.EncodeToString(version[:8]), nil
}

// During rotation the operator retains the old encryption secrets in this
// bounded JSON array until every session and receipt using them is resolved.
func walletPaymentReadKey(version string) ([]byte, error) {
	key, current, err := walletPaymentKey()
	if err == nil && current == version {
		return key, nil
	}
	var previous []string
	raw := os.Getenv("WALLET_PAYMENT_PREVIOUS_SECRETS")
	if len(raw) > 16384 || raw == "" || common.UnmarshalJsonStr(raw, &previous) != nil || len(previous) > 16 {
		return nil, ErrWalletSecretUnavailable
	}
	for _, secret := range previous {
		if len(secret) < 32 {
			continue
		}
		candidate := sha256.Sum256([]byte("wallet-payment-v1\x00" + secret))
		fingerprint := sha256.Sum256(candidate[:])
		if hex.EncodeToString(fingerprint[:8]) == version {
			return candidate[:], nil
		}
	}
	return nil, ErrWalletSecretUnavailable
}
func WalletPaymentSecretsReady() bool { _, _, err := walletPaymentKey(); return err == nil }
func WalletPaymentCredentialVersion() (string, error) {
	_, version, err := walletPaymentKey()
	return version, err
}
func SealWalletPayment(value any, scope string) (string, error) {
	key, version, err := walletPaymentKey()
	if err != nil {
		return "", err
	}
	plain, err := common.Marshal(value)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, plain, []byte(scope))
	return version + "." + base64.RawURLEncoding.EncodeToString(sealed), nil
}
func OpenWalletPayment(sealed, scope string, value any) error {
	parts := strings.SplitN(sealed, ".", 2)
	if len(parts) != 2 {
		return ErrWalletSecretUnavailable
	}
	key, err := walletPaymentReadKey(parts[0])
	if err != nil {
		return ErrWalletSecretUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	blob, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(blob) < aead.NonceSize() {
		return ErrWalletSecretUnavailable
	}
	plain, err := aead.Open(nil, blob[:aead.NonceSize()], blob[aead.NonceSize():], []byte(scope))
	if err != nil {
		return ErrWalletSecretUnavailable
	}
	return common.Unmarshal(plain, value)
}
func SignWalletQuote(value any) (string, error) {
	key, _, err := walletPaymentKey()
	if err != nil {
		return "", err
	}
	body, err := common.Marshal(value)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	h := hmac.New(sha256.New, key)
	h.Write([]byte("wallet-quote-v1." + payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil)), nil
}
func VerifyWalletQuote(token string, value any) error {
	key, _, err := walletPaymentKey()
	if err != nil {
		return err
	}
	if len(token) > 16384 {
		return errors.New("invalid quote")
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return errors.New("invalid quote")
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte("wallet-quote-v1." + parts[0]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, h.Sum(nil)) {
		return errors.New("invalid quote")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return err
	}
	return common.Unmarshal(body, value)
}
