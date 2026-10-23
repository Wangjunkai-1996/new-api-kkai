package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

func invoiceID(prefix string) (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func invoiceKeys() (string, map[string][]byte, error) {
	id := strings.TrimSpace(os.Getenv("KKAI_INVOICE_KEY_ID"))
	var encoded map[string]string
	if id == "" || len(id) > 48 || strings.ContainsAny(id, ".:") {
		return "", nil, invoiceError(503, "ENCRYPTION_UNAVAILABLE")
	}
	raw, err := invoiceSecret("KKAI_INVOICE_KEYS")
	if err != nil || common.UnmarshalJsonStr(raw, &encoded) != nil {
		return "", nil, invoiceError(503, "ENCRYPTION_UNAVAILABLE")
	}
	keys := make(map[string][]byte, len(encoded))
	for k, v := range encoded {
		key, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(key) != 32 {
			return "", nil, invoiceError(503, "ENCRYPTION_UNAVAILABLE")
		}
		keys[k] = key
	}
	if len(keys[id]) != 32 {
		return "", nil, invoiceError(503, "ENCRYPTION_UNAVAILABLE")
	}
	return id, keys, nil
}
func invoiceEncrypt(aad string, value any) (string, error) {
	id, keys, err := invoiceKeys()
	if err != nil {
		return "", err
	}
	raw, err := common.Marshal(value)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(keys[id])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, raw, []byte(aad))
	return "v1." + id + "." + base64.RawURLEncoding.EncodeToString(ciphertext), nil
}
func invoiceDecrypt(aad, value string, target any) error {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return invoiceError(503, "ENCRYPTION_UNAVAILABLE")
	}
	_, keys, err := invoiceKeys()
	if err != nil {
		return err
	}
	key := keys[parts[1]]
	if len(key) != 32 {
		return invoiceError(503, "ENCRYPTION_UNAVAILABLE")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return invoiceError(503, "ENCRYPTION_UNAVAILABLE")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	if len(raw) < gcm.NonceSize() {
		return invoiceError(503, "ENCRYPTION_UNAVAILABLE")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], []byte(aad))
	if err != nil {
		return invoiceError(503, "ENCRYPTION_UNAVAILABLE")
	}
	if err = common.Unmarshal(plain, target); err != nil {
		return invoiceError(503, "ENCRYPTION_UNAVAILABLE")
	}
	return nil
}
func invoiceAAD(tenant int64, user int, id, kind string) string {
	return fmt.Sprintf("invoice:v1:%d:%d:%s:%s", tenant, user, id, kind)
}
