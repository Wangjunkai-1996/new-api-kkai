package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrKKAIRiskFingerprintMismatch = errors.New("KKAI risk fingerprint mismatch")

func DisableKKAIRiskToken(tx *gorm.DB, tokenID int, userID int, fingerprint string) (bool, error) {
	var token Token
	query := lockForUpdate(tx).Where("id = ?", tokenID)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.First(&token).Error; err != nil {
		return false, err
	}
	if !matchesKKAIRiskFingerprint(token.Key, fingerprint) {
		return false, ErrKKAIRiskFingerprintMismatch
	}
	if token.Status == common.TokenStatusDisabled {
		return false, nil
	}
	if err := tx.Model(&Token{}).Where("id = ?", token.Id).
		Update("status", common.TokenStatusDisabled).Error; err != nil {
		return false, err
	}
	return true, nil
}

func DisableKKAIRiskUser(tx *gorm.DB, userID int) (bool, bool, error) {
	var user User
	if err := lockForUpdate(tx).Where("id = ?", userID).First(&user).Error; err != nil {
		return false, false, err
	}
	if user.Role >= common.RoleAdminUser {
		return false, true, nil
	}
	if user.Status == common.UserStatusDisabled {
		return false, false, nil
	}
	if err := tx.Model(&User{}).Where("id = ?", user.Id).
		Update("status", common.UserStatusDisabled).Error; err != nil {
		return false, false, err
	}
	return true, false, nil
}

func ValidateKKAIRiskChannel(tx *gorm.DB, channelID int, fingerprint string) error {
	var channel Channel
	if err := lockForUpdate(tx).Where("id = ?", channelID).First(&channel).Error; err != nil {
		return err
	}
	for _, key := range channel.GetKeys() {
		if matchesKKAIRiskFingerprint(key, fingerprint) {
			return nil
		}
	}
	return ErrKKAIRiskFingerprintMismatch
}

func DisableKKAIRiskChannel(tx *gorm.DB, channelID int, fingerprint string) (bool, error) {
	var channel Channel
	if err := lockForUpdate(tx).Where("id = ?", channelID).First(&channel).Error; err != nil {
		return false, err
	}
	if !matchesKKAIRiskFingerprint(channel.Key, fingerprint) {
		return false, ErrKKAIRiskFingerprintMismatch
	}
	if channel.Status == common.ChannelStatusAutoDisabled {
		return false, nil
	}
	if err := tx.Model(&Channel{}).Where("id = ?", channel.Id).
		Update("status", common.ChannelStatusAutoDisabled).Error; err != nil {
		return false, err
	}
	return true, nil
}

func matchesKKAIRiskFingerprint(secret string, fingerprint string) bool {
	want := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fingerprint)), "sha256:")
	if len(want) != sha256.Size*2 {
		return false
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:]) == want
}
