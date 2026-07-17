package topuprecovery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func (service *Service) Plan(ctx context.Context, activeFromID, cutoffID int64) (*Manifest, error) {
	if err := service.validate(activeFromID, cutoffID); err != nil {
		return nil, err
	}
	sources, err := service.loadMissingSources(ctx, activeFromID, cutoffID)
	if err != nil {
		return nil, err
	}
	manifest := &Manifest{
		SchemaVersion:  SchemaVersion,
		ToolVersion:    ToolVersion,
		SourceRevision: service.sourceRevision,
		ActiveFromID:   activeFromID,
		CutoffID:       cutoffID,
		GeneratedAt:    service.now().Unix(),
		Orders:         make([]OrderEvidence, 0, len(sources)),
	}
	for _, source := range sources {
		evidence, err := service.collectEvidence(ctx, source)
		if err != nil {
			return nil, fmt.Errorf("topup %d: %w", source.ID, err)
		}
		manifest.Orders = append(manifest.Orders, evidence)
	}
	if err := SealManifest(manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (service *Service) Apply(ctx context.Context, manifest *Manifest, expectedSHA256 string) (*Result, error) {
	if err := service.validateManifest(manifest, expectedSHA256); err != nil {
		return nil, err
	}
	sources, err := service.preflightManifest(ctx, manifest)
	if err != nil {
		return nil, err
	}
	result := &Result{
		Mode:           "apply",
		ManifestSHA256: manifest.SHA256,
		OrderCount:     len(manifest.Orders),
	}
	err = service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for index, evidence := range manifest.Orders {
			source := sources[index]
			updated := tx.Model(&model.TopUp{}).
				Where("id = ? AND user_id = ? AND trade_no = ? AND payment_provider = ? AND status = ? AND complete_time = 0",
					source.ID, source.UserID, source.TradeNo, source.PaymentProvider, source.Status).
				Update("complete_time", evidence.CompletedAt)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected == 1 {
				result.UpdatedCount++
				continue
			}
			current, err := loadSourceByID(tx, evidence.TopUpID)
			if err != nil {
				return err
			}
			if err := validateSource(current, evidence, true); err != nil {
				return err
			}
			result.AlreadySetCount++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (service *Service) Verify(ctx context.Context, manifest *Manifest, expectedSHA256 string) (*Result, error) {
	if err := service.validateManifest(manifest, expectedSHA256); err != nil {
		return nil, err
	}
	if _, err := service.preflightManifest(ctx, manifest); err != nil {
		return nil, err
	}
	for _, evidence := range manifest.Orders {
		source, err := loadSourceByID(service.db.WithContext(ctx), evidence.TopUpID)
		if err != nil {
			return nil, err
		}
		if source.CompleteTime != evidence.CompletedAt {
			return nil, fmt.Errorf("topup %d completion time is not applied", evidence.TopUpID)
		}
	}
	return &Result{
		Mode:           "verify",
		ManifestSHA256: manifest.SHA256,
		OrderCount:     len(manifest.Orders),
		VerifiedCount:  len(manifest.Orders),
	}, nil
}

func (service *Service) validate(activeFromID, cutoffID int64) error {
	if service == nil || service.db == nil || service.provider == nil ||
		!gitSHA1Pattern.MatchString(service.sourceRevision) || activeFromID <= 0 || cutoffID < activeFromID {
		return ErrInvalidManifest
	}
	return nil
}

func (service *Service) validateManifest(manifest *Manifest, expectedSHA256 string) error {
	if service == nil || service.db == nil || service.provider == nil {
		return ErrInvalidManifest
	}
	return ValidateManifest(manifest, expectedSHA256, service.sourceRevision)
}

func (service *Service) loadMissingSources(ctx context.Context, activeFromID, cutoffID int64) ([]topUpSource, error) {
	var sources []topUpSource
	err := service.db.WithContext(ctx).Table("top_ups").
		Select("top_ups.id, top_ups.user_id, top_ups.trade_no, top_ups.payment_provider, top_ups.create_time, top_ups.complete_time, top_ups.status").
		Joins("JOIN users ON users.id = top_ups.user_id").
		Where("top_ups.id >= ? AND top_ups.id <= ?", activeFromID, cutoffID).
		Where("top_ups.status = ?", common.TopUpStatusSuccess).
		Where("COALESCE(top_ups.complete_time, 0) = 0").
		Where("users.inviter_id > 0").
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

func (service *Service) collectEvidence(ctx context.Context, source topUpSource) (OrderEvidence, error) {
	providerOrder, err := service.provider.Lookup(ctx, source.TradeNo)
	if err != nil {
		return OrderEvidence{}, err
	}
	if providerOrder.CompletedAt < source.CreateTime || providerOrder.CompletedAt > service.now().Add(5*time.Minute).Unix() {
		return OrderEvidence{}, ErrInvalidProviderEvidence
	}
	sourceSHA256, err := sourceDigest(source)
	if err != nil {
		return OrderEvidence{}, err
	}
	providerSHA256, err := providerDigest(providerOrder)
	if err != nil {
		return OrderEvidence{}, err
	}
	return OrderEvidence{
		TopUpID:                source.ID,
		UserID:                 source.UserID,
		TradeNoSHA256:          hashString(source.TradeNo),
		SourceRowSHA256:        sourceSHA256,
		ProviderResponseSHA256: providerSHA256,
		CompletedAt:            providerOrder.CompletedAt,
	}, nil
}

func (service *Service) preflightManifest(ctx context.Context, manifest *Manifest) ([]topUpSource, error) {
	sources := make([]topUpSource, 0, len(manifest.Orders))
	for _, evidence := range manifest.Orders {
		source, err := loadSourceByID(service.db.WithContext(ctx), evidence.TopUpID)
		if err != nil {
			return nil, err
		}
		if err := validateSource(source, evidence, true); err != nil {
			return nil, err
		}
		providerOrder, err := service.provider.Lookup(ctx, source.TradeNo)
		if err != nil {
			return nil, fmt.Errorf("topup %d: %w", source.ID, err)
		}
		providerSHA256, err := providerDigest(providerOrder)
		if err != nil {
			return nil, err
		}
		if providerOrder.CompletedAt != evidence.CompletedAt || providerSHA256 != evidence.ProviderResponseSHA256 {
			return nil, fmt.Errorf("topup %d provider evidence drifted", source.ID)
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func loadSourceByID(db *gorm.DB, topUpID int64) (topUpSource, error) {
	var source topUpSource
	err := db.Table("top_ups").
		Select("top_ups.id, top_ups.user_id, top_ups.trade_no, top_ups.payment_provider, top_ups.create_time, top_ups.complete_time, top_ups.status").
		Joins("JOIN users ON users.id = top_ups.user_id AND users.inviter_id > 0").
		Where("top_ups.id = ?", topUpID).
		Take(&source).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return topUpSource{}, fmt.Errorf("topup %d is no longer eligible", topUpID)
	}
	return source, err
}

func validateSource(source topUpSource, evidence OrderEvidence, allowApplied bool) error {
	if source.ID != evidence.TopUpID || source.UserID != evidence.UserID ||
		source.PaymentProvider != model.PaymentProviderEpay || source.Status != common.TopUpStatusSuccess ||
		hashString(source.TradeNo) != evidence.TradeNoSHA256 {
		return fmt.Errorf("topup %d source identity drifted", evidence.TopUpID)
	}
	sourceSHA256, err := sourceDigest(source)
	if err != nil {
		return err
	}
	if sourceSHA256 != evidence.SourceRowSHA256 {
		return fmt.Errorf("topup %d source row drifted", evidence.TopUpID)
	}
	if source.CompleteTime != 0 && (!allowApplied || source.CompleteTime != evidence.CompletedAt) {
		return fmt.Errorf("topup %d completion time drifted", evidence.TopUpID)
	}
	return nil
}
