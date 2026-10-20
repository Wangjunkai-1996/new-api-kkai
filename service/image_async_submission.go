package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"gorm.io/gorm"
)

// Keep the wire fields explicit: embedding ImageStudioSubmissionRequest would
// promote its custom UnmarshalJSON and silently discard mode and asset_ids.
type ImageAsyncSubmissionRequest struct {
	TokenID    int            `json:"token_id"`
	Model      string         `json:"model"`
	Prompt     string         `json:"prompt"`
	Parameters map[string]any `json:"parameters"`
	SampleID   *int64         `json:"sample_id,omitempty"`
	QuoteToken string         `json:"quote_token,omitempty"`
	Mode       string         `json:"mode"`
	AssetIDs   []int64        `json:"asset_ids,omitempty"`
}

type ImageExecutionPayload struct {
	Version    int                             `json:"version"`
	Submission NormalizedImageStudioSubmission `json:"submission"`
	Quote      ImageStudioQuoteClaims          `json:"quote"`
	AssetIDs   []int64                         `json:"asset_ids"`
	ClientIP   string                          `json:"client_ip"`
}

func (request ImageAsyncSubmissionRequest) IdentityHash() (string, error) {
	request.QuoteToken = ""
	request.Model = strings.TrimSpace(request.Model)
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.Mode == "" {
		request.Mode = ImageStudioModeGeneration
	}
	encoded, err := common.Marshal(request)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(append([]byte("image-async:v1:"), encoded...))
	return hex.EncodeToString(digest[:]), nil
}

// Replay is deliberately independent of today's quote, asset, model and token
// state. Authentication/ownership and the original submission identity suffice.
func ReplayAsyncImageSubmission(ctx context.Context, db *gorm.DB, userID int, key, hash string) (*model.KKAIImageGeneration, bool, error) {
	key = strings.TrimSpace(key)
	if db == nil || userID <= 0 || key == "" || len(key) > 128 || !validImageStudioHash(hash) {
		return nil, false, ErrInvalidIdempotencyRequest
	}
	var binding model.KKAIIdempotencyKey
	err := db.WithContext(ctx).Where(map[string]any{"user_id": userID, "operation": model.ImageIdempotencyOperationSubmit, "key": key}).Take(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if binding.RequestHash != hash {
		return nil, true, ErrIdempotencyConflict
	}
	if binding.ResourceID == "" {
		return nil, true, ErrIdempotencyInProgress
	}
	if binding.ResourceType != model.ImageIdempotencyResourceGeneration {
		return nil, true, ErrIdempotencyBindingConflict
	}
	id, err := strconv.ParseInt(binding.ResourceID, 10, 64)
	if err != nil || id <= 0 {
		return nil, true, ErrIdempotencyBindingConflict
	}
	generation, err := GetOwnedImageGeneration(ctx, db, userID, id)
	return generation, true, err
}

func NormalizeAsyncImageSubmission(ctx context.Context, db *gorm.DB, userID int, request ImageAsyncSubmissionRequest) (*NormalizedImageStudioSubmission, error) {
	references, err := readyImageReferences(ctx, db, userID, request.AssetIDs)
	if err != nil {
		return nil, err
	}
	legacy := ImageStudioSubmissionRequest{
		TokenID: request.TokenID, Model: request.Model, Prompt: request.Prompt, Parameters: request.Parameters,
		SampleID: request.SampleID, QuoteToken: request.QuoteToken, Mode: request.Mode, References: references,
	}
	return NormalizeImageStudioSubmission(ctx, db, userID, legacy)
}

func readyImageReferences(ctx context.Context, db *gorm.DB, userID int, ids []int64) ([]ImageStudioReferenceMetadata, error) {
	if len(ids) > MaxImageStudioReferenceImages {
		return nil, ErrInvalidImageStudioSubmission
	}
	references := make([]ImageStudioReferenceMetadata, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return nil, ErrInvalidImageStudioSubmission
		}
		seen[id] = true
		var asset model.KKAIImageAsset
		err := db.WithContext(ctx).Where("id = ? AND owner_user_id = ? AND kind = ? AND state = ? AND deleted_at = 0", id, userID, model.ImageAssetKindReference, model.ImageAssetStateReady).Take(&asset).Error
		if err != nil {
			return nil, ErrImageUploadConflict
		}
		references = append(references, ImageStudioReferenceMetadata{SHA256: asset.SHA256, SizeBytes: asset.SizeBytes})
	}
	return references, nil
}

// Acceptance commits the reservation, task, inputs and outbox together. No
// upstream or object-store request is permitted inside this transaction.
func AcceptAsyncImageSubmission(ctx context.Context, db *gorm.DB, request ImageAsyncSubmissionRequest, normalized *NormalizedImageStudioSubmission, key, requestID, clientIP string) (*model.KKAIImageGeneration, error) {
	if normalized == nil || normalized.UserID <= 0 {
		return nil, ErrInvalidImageStudioSubmission
	}
	hash, err := request.IdentityHash()
	if err != nil {
		return nil, err
	}
	var generation *model.KKAIImageGeneration
	var membershipRecord *model.MembershipBillingRecord
	err = model.MembershipTransactionWithDB(ctx, db, func(tx *gorm.DB) error {
		membershipRecord = nil
		if err := model.LockImageStudioCapacityTx(tx); err != nil {
			return err
		}
		replay, found, err := ReplayAsyncImageSubmission(ctx, tx, normalized.UserID, key, hash)
		if err != nil {
			return err
		}
		if found {
			generation = replay
			return nil
		}
		if !image_studio_setting.Get().AsyncSubmissionEnabled {
			return ErrImageUploadUnavailable
		}
		// Revalidate ownership/readiness under the same admission lock used by GC.
		references, err := readyImageReferences(ctx, tx, normalized.UserID, request.AssetIDs)
		if err != nil {
			return err
		}
		if len(references) != len(normalized.References) {
			return ErrImageUploadConflict
		}
		for i := range references {
			if references[i] != normalized.References[i] {
				return ErrImageUploadConflict
			}
		}
		if _, err := ValidateImageStudioToken(ctx, tx, normalized.UserID, normalized.TokenID, normalized.Model, clientIP); err != nil {
			return err
		}
		quote, err := ValidateImageStudioQuote(normalized, time.Now())
		if err != nil {
			return err
		}
		if quote.ImagePricingSnapshot == nil {
			return ErrImageModelBillingUnsupported
		}
		memberBilling, err := model.ShouldUseMembershipBillingTx(tx, normalized.UserID)
		if err != nil {
			return err
		}
		if memberBilling != (quote.MembershipPricing != nil) {
			return ErrImageStudioQuoteStale
		}
		now := time.Now()
		binding := model.KKAIIdempotencyKey{UserID: normalized.UserID, Operation: model.ImageIdempotencyOperationSubmit, Key: strings.TrimSpace(key), RequestHash: hash, CreatedAt: now.Unix(), ExpiresAt: now.Add(DefaultIdempotencyTTL).Unix()}
		if err := tx.Create(&binding).Error; err != nil {
			return err
		}
		generation, err = CreateImageGeneration(ctx, tx, normalized, requestID, binding.ID)
		if err != nil {
			return err
		}
		var user model.User
		if err := tx.First(&user, normalized.UserID).Error; err != nil {
			return err
		}
		if memberBilling {
			input, err := imageStudioMembershipReservation(normalized, quote)
			if err != nil {
				return err
			}
			membershipRecord, err = model.ReserveMembershipImageGenerationBillingTx(tx, generation.ID, input)
			if err != nil {
				return err
			}
			if err := tx.First(generation, generation.ID).Error; err != nil {
				return err
			}
		} else {
			mutation, err := ReserveImageGenerationBillingForUserTx(ctx, tx, generation.ID, quote.Quota, user.GetSetting().BillingPreference, quote.Quota == 0)
			if err != nil {
				return err
			}
			generation = mutation.Generation
		}
		for position, id := range request.AssetIDs {
			reference := model.KKAIImageGenerationReference{GenerationID: generation.ID, Position: position, AssetID: id, CreatedAt: now.Unix()}
			if err := tx.Create(&reference).Error; err != nil {
				return err
			}
		}
		payload := ImageExecutionPayload{Version: 1, Submission: *normalized, Quote: quote, AssetIDs: request.AssetIDs, ClientIP: clientIP}
		payload.Submission.QuoteToken = ""
		encoded, err := common.Marshal(payload)
		if err != nil {
			return err
		}
		eventKey := "image-generate:" + strconv.FormatInt(generation.ID, 10)
		if err := EnqueueKKAIOutboxEvent(ctx, tx, eventKey, ImageGenerateTopic, strconv.FormatInt(generation.ID, 10), now, map[string]int64{"generation_id": generation.ID}); err != nil {
			return err
		}
		var event model.KKAIOutboxEvent
		if err := tx.Where("event_key = ?", eventKey).Take(&event).Error; err != nil {
			return err
		}
		execution := model.KKAIImageExecution{GenerationID: generation.ID, State: model.ImageGenerationExecutionQueued, EventID: event.ID, Payload: string(encoded), QueueDeadlineAt: now.Add(ImageQueueTimeout).Unix(), CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
		return tx.Create(&execution).Error
	})
	if err != nil {
		return nil, err
	}
	model.MembershipBillingAfterCommit(membershipRecord)
	// Cache reconciliation also has its own durable outbox event from the debit.
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := model.ReconcileImageGenerationBillingCache(cacheCtx, db, generation.ID); err != nil {
		common.SysError("image async billing cache reconciliation deferred")
	}
	return generation, nil
}
