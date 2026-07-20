package topuprecovery

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrOutboxConflict = errors.New("topup rebate outbox conflict")

func (service *Service) LatestCutoff(ctx context.Context, activeFromID int64) (int64, error) {
	if err := service.validate(activeFromID, activeFromID); err != nil {
		return 0, err
	}
	var cutoffID int64
	if err := service.db.WithContext(ctx).Model(&model.TopUp{}).
		Select("COALESCE(MAX(id), 0)").
		Scan(&cutoffID).Error; err != nil {
		return 0, err
	}
	if cutoffID < activeFromID {
		return 0, ErrInvalidManifest
	}
	return cutoffID, nil
}

func (service *Service) Plan(ctx context.Context, activeFromID, cutoffID int64) (*Manifest, error) {
	if err := service.validate(activeFromID, cutoffID); err != nil {
		return nil, err
	}
	sources, err := service.loadEligibleSources(ctx, activeFromID, cutoffID)
	if err != nil {
		return nil, err
	}
	manifest := &Manifest{
		SchemaVersion:  SchemaVersion,
		ToolVersion:    ToolVersion,
		SourceRevision: service.sourceRevision,
		ActiveFromID:   activeFromID,
		CutoffID:       cutoffID,
		QuotaPerUnit:   service.quotaPerUnitString(),
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
	if err := service.preflightManifest(ctx, manifest); err != nil {
		return nil, err
	}
	result := &Result{
		Mode:           "apply",
		ManifestSHA256: manifest.SHA256,
		OrderCount:     len(manifest.Orders),
	}
	err := service.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, evidence := range manifest.Orders {
			source, err := loadSourceByID(tx, evidence.TopUpID, true)
			if err != nil {
				return err
			}
			if err := validateSource(source, evidence, true); err != nil {
				return err
			}
			payload, err := service.validateExpectedEvent(source, evidence)
			if err != nil {
				return err
			}

			if source.CompleteTime == 0 {
				updated := tx.Model(&model.TopUp{}).
					Where("id = ? AND complete_time = 0", evidence.TopUpID).
					Update("complete_time", evidence.CompletedAt)
				if updated.Error != nil {
					return updated.Error
				}
				if updated.RowsAffected != 1 {
					return fmt.Errorf("topup %d completion time drifted during apply", evidence.TopUpID)
				}
				result.UpdatedCount++
			} else {
				result.AlreadySetCount++
			}

			created, err := ensureExpectedOutbox(tx, evidence, payload)
			if err != nil {
				return err
			}
			if created {
				result.OutboxCreatedCount++
			} else {
				result.OutboxAlreadyPresentCount++
			}
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
	if err := service.preflightManifest(ctx, manifest); err != nil {
		return nil, err
	}
	for _, evidence := range manifest.Orders {
		source, err := loadSourceByID(service.db.WithContext(ctx), evidence.TopUpID, false)
		if err != nil {
			return nil, err
		}
		if err := validateSource(source, evidence, true); err != nil {
			return nil, err
		}
		if source.CompleteTime != evidence.CompletedAt {
			return nil, fmt.Errorf("topup %d completion time is not applied", evidence.TopUpID)
		}
		payload, err := service.validateExpectedEvent(source, evidence)
		if err != nil {
			return nil, err
		}
		if err := verifyExpectedOutbox(service.db.WithContext(ctx), evidence, payload); err != nil {
			return nil, err
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
		!gitSHA1Pattern.MatchString(service.sourceRevision) ||
		math.IsNaN(service.quotaPerUnit) || math.IsInf(service.quotaPerUnit, 0) ||
		service.quotaPerUnit <= 0 || activeFromID <= 0 || cutoffID < activeFromID {
		return ErrInvalidManifest
	}
	return nil
}

func (service *Service) validateManifest(manifest *Manifest, expectedSHA256 string) error {
	if service == nil || service.db == nil || service.provider == nil ||
		math.IsNaN(service.quotaPerUnit) || math.IsInf(service.quotaPerUnit, 0) ||
		service.quotaPerUnit <= 0 {
		return ErrInvalidManifest
	}
	if err := ValidateManifest(manifest, expectedSHA256, service.sourceRevision); err != nil {
		return err
	}
	if manifest.QuotaPerUnit != service.quotaPerUnitString() {
		return fmt.Errorf("%w: quota configuration mismatch", ErrInvalidManifest)
	}
	return nil
}

func (service *Service) loadEligibleSources(ctx context.Context, activeFromID, cutoffID int64) ([]topUpSource, error) {
	var missingInviterTopUpIDs []int64
	err := service.db.WithContext(ctx).Table("top_ups").
		Joins("JOIN users AS invitees ON invitees.id = top_ups.user_id").
		Joins("LEFT JOIN users AS inviters ON inviters.id = invitees.inviter_id").
		Where("top_ups.id >= ? AND top_ups.id <= ?", activeFromID, cutoffID).
		Where("top_ups.status = ?", common.TopUpStatusSuccess).
		Where("invitees.inviter_id > 0 AND inviters.id IS NULL").
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
	err = topUpSourceQuery(service.db.WithContext(ctx)).
		Where("top_ups.id >= ? AND top_ups.id <= ?", activeFromID, cutoffID).
		Where("top_ups.status = ?", common.TopUpStatusSuccess).
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
	return db.Table("top_ups").
		Select(columns).
		Joins("JOIN users AS invitees ON invitees.id = top_ups.user_id").
		Joins("JOIN users AS inviters ON inviters.id = invitees.inviter_id").
		Where("invitees.inviter_id > 0")
}

func (service *Service) collectEvidence(ctx context.Context, source topUpSource) (OrderEvidence, error) {
	providerOrder, err := service.provider.Lookup(ctx, source.TradeNo)
	if err != nil {
		return OrderEvidence{}, err
	}
	if providerOrder.CompletedAt < source.CreateTime || providerOrder.CompletedAt > service.now().Add(5*time.Minute).Unix() {
		return OrderEvidence{}, ErrInvalidProviderEvidence
	}
	if source.CompleteTime != 0 && source.CompleteTime != providerOrder.CompletedAt {
		return OrderEvidence{}, fmt.Errorf("stored completion time disagrees with provider evidence")
	}
	sourceSHA256, err := sourceDigest(source)
	if err != nil {
		return OrderEvidence{}, err
	}
	providerSHA256, err := providerDigest(providerOrder)
	if err != nil {
		return OrderEvidence{}, err
	}
	event, payload, err := service.expectedEvent(source, providerOrder.CompletedAt)
	if err != nil {
		return OrderEvidence{}, err
	}
	return OrderEvidence{
		TopUpID:                source.ID,
		UserID:                 source.UserID,
		InviterID:              source.InviterID,
		InviterGroup:           event.InviterGroup,
		CreditedQuota:          event.CreditedQuota,
		EventKey:               event.EventKey,
		EventPayloadSHA256:     hashString(payload),
		TradeNoSHA256:          hashString(source.TradeNo),
		SourceRowSHA256:        sourceSHA256,
		ProviderResponseSHA256: providerSHA256,
		CompletedAt:            providerOrder.CompletedAt,
	}, nil
}

func (service *Service) preflightManifest(ctx context.Context, manifest *Manifest) error {
	for _, evidence := range manifest.Orders {
		source, err := loadSourceByID(service.db.WithContext(ctx), evidence.TopUpID, false)
		if err != nil {
			return err
		}
		if err := validateSource(source, evidence, true); err != nil {
			return err
		}
		if _, err := service.validateExpectedEvent(source, evidence); err != nil {
			return err
		}
		providerOrder, err := service.provider.Lookup(ctx, source.TradeNo)
		if err != nil {
			return fmt.Errorf("topup %d: %w", source.ID, err)
		}
		providerSHA256, err := providerDigest(providerOrder)
		if err != nil {
			return err
		}
		if providerOrder.CompletedAt != evidence.CompletedAt || providerSHA256 != evidence.ProviderResponseSHA256 {
			return fmt.Errorf("topup %d provider evidence drifted", source.ID)
		}
	}
	return nil
}

func loadSourceByID(db *gorm.DB, topUpID int64, lock bool) (topUpSource, error) {
	var source topUpSource
	query := topUpSourceQuery(db).Where("top_ups.id = ?", topUpID)
	if lock && db.Dialector.Name() != "sqlite" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.Take(&source).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return topUpSource{}, fmt.Errorf("topup %d is no longer eligible", topUpID)
	}
	if err != nil {
		return topUpSource{}, err
	}
	return source, nil
}

func validateSource(source topUpSource, evidence OrderEvidence, allowApplied bool) error {
	if source.ID != evidence.TopUpID || source.UserID != evidence.UserID ||
		source.InviterID != evidence.InviterID ||
		normalizeInviterGroup(source.InviterGroup) != evidence.InviterGroup ||
		source.PaymentProvider != model.PaymentProviderEpay ||
		source.Status != common.TopUpStatusSuccess ||
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

func (service *Service) expectedEvent(source topUpSource, completedAt int64) (*model.TopUpCompletedEvent, string, error) {
	creditedQuota, err := model.CreditedQuotaFromTopUpAmount(source.Amount, service.quotaPerUnit)
	if err != nil {
		return nil, "", err
	}
	inviterID := source.InviterID
	event, err := model.NewTopUpCompletedEvent(model.TopUpCompletedEventInput{
		SourceOrderID:   source.ID,
		InviteeID:       source.UserID,
		InviterID:       &inviterID,
		InviterGroup:    source.InviterGroup,
		CreditedQuota:   creditedQuota,
		CompletedAt:     completedAt,
		PaymentProvider: source.PaymentProvider,
	})
	if err != nil {
		return nil, "", err
	}
	encoded, err := common.Marshal(event)
	if err != nil {
		return nil, "", err
	}
	return event, string(encoded), nil
}

func (service *Service) validateExpectedEvent(source topUpSource, evidence OrderEvidence) (string, error) {
	event, payload, err := service.expectedEvent(source, evidence.CompletedAt)
	if err != nil {
		return "", err
	}
	if event.EventKey != evidence.EventKey ||
		event.InviterID == nil || *event.InviterID != evidence.InviterID ||
		event.InviterGroup != evidence.InviterGroup ||
		event.CreditedQuota != evidence.CreditedQuota ||
		hashString(payload) != evidence.EventPayloadSHA256 {
		return "", fmt.Errorf("topup %d rebate event drifted", evidence.TopUpID)
	}
	return payload, nil
}

func ensureExpectedOutbox(tx *gorm.DB, evidence OrderEvidence, payload string) (bool, error) {
	candidate := model.KKAIOutboxEvent{
		EventKey:    evidence.EventKey,
		Topic:       model.KKAIOutboxTopicTopUpCompleted,
		AggregateID: strconv.FormatInt(evidence.TopUpID, 10),
		Payload:     payload,
		Status:      model.KKAIOutboxStatusPending,
		AvailableAt: evidence.CompletedAt,
		CreatedAt:   evidence.CompletedAt,
	}
	inserted := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "event_key"}},
		DoNothing: true,
	}).Create(&candidate)
	if inserted.Error != nil {
		return false, inserted.Error
	}
	if err := verifyExpectedOutbox(tx, evidence, payload); err != nil {
		return false, err
	}
	return inserted.RowsAffected == 1, nil
}

func verifyExpectedOutbox(db *gorm.DB, evidence OrderEvidence, payload string) error {
	var events []model.KKAIOutboxEvent
	if err := db.Where("event_key = ?", evidence.EventKey).Limit(2).Find(&events).Error; err != nil {
		return err
	}
	if len(events) != 1 {
		return fmt.Errorf("%w: topup %d expected exactly one event, found %d", ErrOutboxConflict, evidence.TopUpID, len(events))
	}
	event := events[0]
	validStatus := event.Status == model.KKAIOutboxStatusPending || event.Status == model.KKAIOutboxStatusDelivered
	if event.Topic != model.KKAIOutboxTopicTopUpCompleted ||
		event.AggregateID != strconv.FormatInt(evidence.TopUpID, 10) ||
		event.Payload != payload || hashString(event.Payload) != evidence.EventPayloadSHA256 ||
		event.AvailableAt != evidence.CompletedAt || event.CreatedAt != evidence.CompletedAt ||
		!validStatus {
		return fmt.Errorf("%w: topup %d event content differs", ErrOutboxConflict, evidence.TopUpID)
	}
	return nil
}

func normalizeInviterGroup(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	return value
}
