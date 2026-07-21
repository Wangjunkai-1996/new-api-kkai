package topuprecovery

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCandidateSetDrift = errors.New("topup recovery candidate set drifted")

type recoveryLockedRow struct {
	ID int64
}

func (service *Service) loadEligibleSources(db *gorm.DB, activeFromID, cutoffID int64) ([]topUpSource, error) {
	var missingInviterTopUpIDs []int64
	err := candidateTopUpQuery(db).
		Joins("LEFT JOIN users AS inviters ON inviters.id = invitees.inviter_id").
		Where("top_ups.id >= ? AND top_ups.id <= ?", activeFromID, cutoffID).
		Where("inviters.id IS NULL").
		Order("top_ups.id ASC").
		Limit(2).
		Pluck("top_ups.id", &missingInviterTopUpIDs).Error
	if err != nil {
		return nil, err
	}
	if len(missingInviterTopUpIDs) > 0 {
		return nil, fmt.Errorf("topup %d references a missing inviter", missingInviterTopUpIDs[0])
	}

	var sources []topUpSource
	err = topUpSourceQuery(db).
		Where("top_ups.id >= ? AND top_ups.id <= ?", activeFromID, cutoffID).
		Order("top_ups.id ASC").
		Scan(&sources).Error
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		if source.PaymentProvider != model.PaymentProviderEpay {
			return nil, fmt.Errorf("topup %d requires unsupported provider evidence", source.ID)
		}
	}
	return sources, nil
}

func candidateTopUpQuery(db *gorm.DB) *gorm.DB {
	return db.Table("top_ups").
		Joins("JOIN users AS invitees ON invitees.id = top_ups.user_id").
		Where("top_ups.status = ?", common.TopUpStatusSuccess).
		Where("invitees.inviter_id > 0").
		Where("NOT EXISTS (SELECT 1 FROM subscription_orders WHERE subscription_orders.trade_no = top_ups.trade_no)")
}

func lockRecoveryCandidateDependencies(tx *gorm.DB, activeFromID, cutoffID int64) error {
	var topUps []recoveryLockedRow
	if err := recoveryLockForUpdate(tx.Table("top_ups").
		Select("id").
		Where("id >= ? AND id <= ?", activeFromID, cutoffID).
		Order("id ASC")).
		Find(&topUps).Error; err != nil {
		return err
	}

	topUpUserIDs := tx.Table("top_ups").
		Select("user_id").
		Where("id >= ? AND id <= ?", activeFromID, cutoffID)
	var invitees []recoveryLockedRow
	if err := recoveryLockForUpdate(tx.Table("users").
		Select("id").
		Where("id IN (?)", topUpUserIDs).
		Order("id ASC")).
		Find(&invitees).Error; err != nil {
		return err
	}

	inviterIDs := tx.Table("users AS invitees").
		Select("invitees.inviter_id").
		Where("invitees.id IN (?)", topUpUserIDs).
		Where("invitees.inviter_id > 0")
	var inviters []recoveryLockedRow
	return recoveryLockForUpdate(tx.Table("users").
		Select("id").
		Where("id IN (?)", inviterIDs).
		Order("id ASC")).
		Find(&inviters).Error
}

func recoveryLockForUpdate(query *gorm.DB) *gorm.DB {
	if query.Dialector.Name() == "sqlite" {
		return query
	}
	return query.Clauses(clause.Locking{Strength: "UPDATE"})
}

func topUpSourceQuery(db *gorm.DB) *gorm.DB {
	inviterGroupColumn := "inviters.`group`"
	if db.Dialector.Name() == "postgres" {
		inviterGroupColumn = "inviters.\"group\""
	}
	columns := fmt.Sprintf(
		"top_ups.id, top_ups.user_id, top_ups.amount, top_ups.trade_no, "+
			"top_ups.payment_provider, top_ups.create_time, top_ups.complete_time, "+
			"top_ups.status, invitees.inviter_id AS inviter_id, "+
			"COALESCE(%s, '') AS inviter_group",
		inviterGroupColumn,
	)
	return candidateTopUpQuery(db).
		Select(columns).
		Joins("JOIN users AS inviters ON inviters.id = invitees.inviter_id")
}

func validateCandidateSet(sources []topUpSource, orders []OrderEvidence) error {
	if len(sources) != len(orders) {
		return fmt.Errorf("%w: manifest has %d orders, database has %d", ErrCandidateSetDrift, len(orders), len(sources))
	}
	for index, source := range sources {
		if source.ID != orders[index].TopUpID {
			return fmt.Errorf(
				"%w: manifest topup %d differs from database topup %d at position %d",
				ErrCandidateSetDrift,
				orders[index].TopUpID,
				source.ID,
				index,
			)
		}
	}
	return nil
}

func (service *Service) validateManifestCandidateSet(db *gorm.DB, manifest *Manifest) error {
	sources, err := service.loadEligibleSources(db, manifest.ActiveFromID, manifest.CutoffID)
	if err != nil {
		return err
	}
	return validateCandidateSet(sources, manifest.Orders)
}
