package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const activeLoginEncryptionKeySlot = "active"

// LoginEncryptionKey stores internal key material used by the browser login
// protocol. It is deliberately separate from administrator-facing options.
type LoginEncryptionKey struct {
	ID            uint   `json:"-" gorm:"primaryKey"`
	Slot          string `json:"-" gorm:"type:varchar(32);not null;uniqueIndex"`
	PrivateKeyPEM string `json:"-" gorm:"type:text;not null"`
}

// LoadPasswordEncryption loads an existing key without changing database state.
func LoadPasswordEncryption() error {
	var stored LoginEncryptionKey
	if err := DB.Where("slot = ?", activeLoginEncryptionKeySlot).First(&stored).Error; err != nil {
		return fmt.Errorf("read password encryption key: %w", err)
	}
	if err := common.LoadPasswordEncryptionPrivateKey(stored.PrivateKeyPEM); err != nil {
		return fmt.Errorf("load persisted password encryption key: %w", err)
	}
	return nil
}

// InitPasswordEncryption provisions a missing shared key. Runtime calls are
// restricted to automatic migration; managed deployments provision explicitly.
func InitPasswordEncryption() error {
	if err := LoadPasswordEncryption(); !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	privateKeyPEM, err := common.GeneratePasswordEncryptionPrivateKey()
	if err != nil {
		return err
	}
	candidate := LoginEncryptionKey{
		Slot:          activeLoginEncryptionKeySlot,
		PrivateKeyPEM: privateKeyPEM,
	}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "slot"}},
		DoNothing: true,
	}).Create(&candidate).Error; err != nil {
		return fmt.Errorf("persist password encryption key: %w", err)
	}

	return LoadPasswordEncryption()
}
