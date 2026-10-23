package service

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func invoiceDescription(value string) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 1000 && !strings.ContainsRune(value, 0)
}
func (s InvoiceService) event(tx *gorm.DB, scope InvoiceScope, row model.InvoiceRequest, kind, description string, attachmentID int64) error {
	id, err := invoiceID("ie_")
	if err != nil {
		return err
	}
	e := model.InvoiceEvent{PublicID: id, TenantID: row.TenantID, RequestID: row.ID, UserID: row.UserID, ActorID: scope.UserID, ActorAdmin: scope.Admin, Kind: kind, Status: row.Status, DeliveryVersion: row.CurrentDeliveryVersion, AttachmentID: attachmentID, CreatedAt: time.Now().Unix()}
	if description != "" {
		e.DescriptionCipher, err = invoiceEncrypt(invoiceAAD(row.TenantID, row.UserID, id, "event"), description)
		if err != nil {
			return err
		}
	}
	if err = tx.Create(&e).Error; err != nil {
		return err
	}
	return EnqueueKKAIOutboxEvent(tx.Statement.Context, tx, "invoice:event:"+id, InvoiceNotificationTopic, strconv.FormatInt(row.ID, 10), time.Time{}, map[string]int64{"event_id": e.ID})
}
func (s InvoiceService) Create(ctx context.Context, scope InvoiceScope, key string, input InvoiceCreateInput) (InvoiceDTO, error) {
	if err := invoiceValidateScope(scope); err != nil {
		return InvoiceDTO{}, err
	}
	var resultID string
	err := s.database(ctx).Transaction(func(tx *gorm.DB) error {
		operation, replay, err := invoiceOperation(tx, scope, "create", key, input)
		if err != nil {
			return err
		}
		if replay {
			resultID = operation.ResultID
			return nil
		}
		p := s.Policy()
		if !p.Enabled {
			return invoiceError(403, p.DisabledReason)
		}
		if !input.UserConfirmed || (input.ProfileID == "") == (input.Profile == nil) || utf8.RuneCountInString(input.Note) > 500 || !utf8.ValidString(input.Note) {
			return invoiceError(400, "VALIDATION_FAILED")
		}
		quote, err := s.quote(ctx, scope, tx, input.InvoiceQuoteInput, true, 0)
		if err != nil {
			return err
		}
		if !quote.Eligible {
			return invoiceError(409, "PAYMENT_CHANGED")
		}
		profile := InvoiceProfileData{}
		if input.ProfileID != "" {
			var saved model.InvoiceProfile
			if err = tx.Where("tenant_id = ? AND user_id = ? AND public_id = ? AND deleted_at = 0", scope.TenantID, scope.UserID, input.ProfileID).First(&saved).Error; err != nil {
				return err
			}
			if err = invoiceDecrypt(invoiceAAD(scope.TenantID, scope.UserID, saved.PublicID, "profile"), saved.DataCipher, &profile); err != nil {
				return err
			}
		} else {
			profile = *input.Profile
		}
		if err = invoiceValidateProfile(&profile, p, input.InvoiceType); err != nil {
			return err
		}
		resultID, err = invoiceID("INV-")
		if err != nil {
			return err
		}
		now := time.Now().Unix()
		row := model.InvoiceRequest{PublicID: resultID, TenantID: scope.TenantID, UserID: scope.UserID, Status: model.InvoicePendingReview, Version: 1, PolicyVersion: p.PolicyVersion, SellerID: p.SellerID, SellerName: p.SellerName, TaxGroup: p.TaxGroup, ProfileID: input.ProfileID, InvoiceType: input.InvoiceType, Currency: quote.Currency, CurrencyExponent: 2, CreatedAt: now, UpdatedAt: now}
		row.AmountMinor, _ = strconv.ParseInt(quote.AmountMinor, 10, 64)
		row.ProfileCipher, err = invoiceEncrypt(invoiceAAD(scope.TenantID, scope.UserID, resultID, "profile"), profile)
		if err != nil {
			return err
		}
		row.NoteCipher, err = invoiceEncrypt(invoiceAAD(scope.TenantID, scope.UserID, resultID, "note"), input.Note)
		if err != nil {
			return err
		}
		if err = tx.Create(&row).Error; err != nil {
			return err
		}
		sort.Slice(quote.Items, func(i, j int) bool { return quote.Items[i].canonicalKey < quote.Items[j].canonicalKey })
		for _, c := range quote.Items {
			claim := model.InvoiceOrderClaim{TenantID: scope.TenantID, CanonicalPaymentKey: c.canonicalKey, RequestID: row.ID, CreatedAt: now}
			if err = tx.Create(&claim).Error; err != nil {
				return invoiceError(409, "ORDER_ALREADY_CLAIMED")
			}
			item := model.InvoiceRequestItem{TenantID: scope.TenantID, UserID: scope.UserID, RequestID: row.ID, CanonicalPaymentKey: c.canonicalKey, SourceType: c.SourceType, SourceID: c.sourceID, OrderID: c.OrderID, OrderNo: c.OrderNo, AmountMinor: c.amount, Currency: c.Currency, PaidAt: c.paidAt, PaymentLabel: c.PaymentLabel, TaxGroup: p.TaxGroup}
			if err = tx.Create(&item).Error; err != nil {
				return err
			}
		}
		if err = s.event(tx, scope, row, "submitted", "", 0); err != nil {
			return err
		}
		return invoiceFinishOperation(tx, operation, resultID, "")
	})
	if err != nil {
		return InvoiceDTO{}, err
	}
	return s.Get(ctx, scope, resultID)
}
func (s InvoiceService) bindEvidence(tx *gorm.DB, scope InvoiceScope, row model.InvoiceRequest, id string) (int64, error) {
	if id == "" {
		return 0, nil
	}
	var attachment model.InvoiceAttachment
	if err := model.InvoiceLockForUpdate(tx).Where("tenant_id = ? AND request_id = ? AND public_id = ? AND uploader_id = ? AND purpose = ?", row.TenantID, row.ID, id, scope.UserID, "dispute_evidence").First(&attachment).Error; err != nil {
		return 0, err
	}
	if attachment.Status != "validated" || attachment.BoundAt != 0 || attachment.ExpiresAt <= time.Now().Unix() {
		return 0, invoiceError(409, "ATTACHMENT_ALREADY_BOUND")
	}
	if err := tx.Model(&attachment).Update("bound_at", time.Now().Unix()).Error; err != nil {
		return 0, err
	}
	return attachment.ID, nil
}
func (s InvoiceService) revalidate(ctx context.Context, tx *gorm.DB, scope InvoiceScope, row model.InvoiceRequest, snapshot *invoicePaymentSnapshot) error {
	var items []model.InvoiceRequestItem
	if err := tx.Where("tenant_id = ? AND request_id = ?", row.TenantID, row.ID).Order("canonical_payment_key ASC").Find(&items).Error; err != nil {
		return err
	}
	input := InvoiceQuoteInput{InvoiceType: row.InvoiceType}
	for _, i := range items {
		input.Items = append(input.Items, InvoiceOrderRef{SourceType: i.SourceType, OrderID: i.OrderID})
	}
	owner := InvoiceScope{TenantID: row.TenantID, UserID: row.UserID}
	quote, err := s.quote(ctx, owner, tx, input, false, row.ID, snapshot)
	if err != nil {
		return err
	}
	if !quote.Eligible || quote.Currency != row.Currency || quote.AmountMinor != strconv.FormatInt(row.AmountMinor, 10) {
		return invoiceError(409, "PAYMENT_CHANGED")
	}
	return nil
}
func (s InvoiceService) lockPayments(tx *gorm.DB, row model.InvoiceRequest) (*invoicePaymentSnapshot, error) {
	var items []model.InvoiceRequestItem
	if err := tx.Where("tenant_id = ? AND request_id = ?", row.TenantID, row.ID).Order("canonical_payment_key ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	refs := make([]InvoiceOrderRef, 0, len(items))
	for _, item := range items {
		refs = append(refs, InvoiceOrderRef{SourceType: item.SourceType, OrderID: item.OrderID})
	}
	return s.lockQuotePayments(tx, InvoiceScope{TenantID: row.TenantID, UserID: row.UserID}, refs)
}
func (s InvoiceService) Mutate(ctx context.Context, scope InvoiceScope, id, action, key string, input InvoiceActionInput) (InvoiceDTO, error) {
	if err := invoiceValidateScope(scope); err != nil {
		return InvoiceDTO{}, err
	}
	var committedCode string
	err := s.database(ctx).Transaction(func(tx *gorm.DB) error {
		var initial model.InvoiceRequest
		if err := invoiceRequestQuery(tx, scope, id).First(&initial).Error; err != nil {
			return err
		}
		operation, replay, err := invoiceOperation(tx, scope, action+":"+id+":"+input.DisputeID, key, input)
		if err != nil {
			return err
		}
		if replay {
			committedCode = operation.ResultCode
			return nil
		}
		snapshot, err := s.lockPayments(tx, initial)
		if err != nil {
			return err
		}
		var row model.InvoiceRequest
		if err = model.InvoiceLockForUpdate(invoiceRequestQuery(tx, scope, id)).First(&row).Error; err != nil {
			return err
		}
		if action == "confirm" && !scope.Admin && row.Status == model.InvoiceConfirmed && input.DeliveryVersion == row.CurrentDeliveryVersion {
			return invoiceFinishOperation(tx, operation, id, "")
		}
		if row.Version != input.Version {
			return invoiceError(409, "VERSION_CONFLICT")
		}
		if !slices.Contains(InvoiceAllowedActions(row, scope.Admin), action) {
			if action == "confirm" && !scope.Admin && row.Status == model.InvoiceConfirmed && input.DeliveryVersion == row.CurrentDeliveryVersion {
				return invoiceFinishOperation(tx, operation, id, "")
			}
			return invoiceError(409, "INVALID_STATE")
		}
		now := time.Now().Unix()
		oldVersion := row.Version
		description := ""
		attachmentID := int64(0)
		if slices.Contains([]string{"approve", "deliver", "reissue"}, action) {
			p := s.Policy()
			if !p.Enabled {
				return invoiceError(403, p.DisabledReason)
			}
			if p.SellerID != row.SellerID || p.TaxGroup != row.TaxGroup || p.PolicyVersion != row.PolicyVersion {
				return invoiceError(409, "POLICY_CHANGED")
			}
		}
		if slices.Contains([]string{"approve", "deliver", "reissue", "resume"}, action) {
			if err = s.revalidate(ctx, tx, scope, row, snapshot); err != nil {
				var business *InvoiceError
				if !errors.As(err, &business) {
					return err
				}
				if row.Status != model.InvoiceFinanceHold {
					row.HoldFromStatus = row.Status
				}
				row.Status = model.InvoiceFinanceHold
				row.ReasonCipher, err = invoiceEncrypt(invoiceAAD(row.TenantID, row.UserID, row.PublicID, "reason"), "Payment facts need finance review")
				if err != nil {
					return err
				}
				committedCode = "PAYMENT_CHANGED"
			}
		}
		if committedCode == "" {
			switch action {
			case "approve":
				row.Status = model.InvoiceApprovedForIssue
				row.ReviewedAt = now
			case "reject":
				if !input.NotIssuedConfirmed || !invoiceDescription(input.Reason) || row.EverDelivered || row.IssuedEvidence {
					return invoiceError(400, "VALIDATION_FAILED")
				}
				description = input.Reason
				row.Status = model.InvoiceRejected
				row.ReviewedAt = now
				row.ReasonCipher, err = invoiceEncrypt(invoiceAAD(row.TenantID, row.UserID, row.PublicID, "reason"), description)
				if err != nil {
					return err
				}
				if err = tx.Where("tenant_id = ? AND request_id = ?", row.TenantID, row.ID).Delete(&model.InvoiceOrderClaim{}).Error; err != nil {
					return err
				}
			case "hold":
				if !invoiceDescription(input.Reason) {
					return invoiceError(400, "VALIDATION_FAILED")
				}
				description = input.Reason
				if row.Status != model.InvoiceFinanceHold {
					row.HoldFromStatus = row.Status
				}
				row.Status = model.InvoiceFinanceHold
				row.ReasonCipher, err = invoiceEncrypt(invoiceAAD(row.TenantID, row.UserID, row.PublicID, "reason"), description)
				if err != nil {
					return err
				}
			case "resume":
				if !invoiceDescription(input.Reason) || !slices.Contains([]string{model.InvoicePendingReview, model.InvoiceApprovedForIssue, model.InvoiceDelivered, model.InvoiceConfirmed, model.InvoiceDisputed, model.InvoiceAwaitingReconfirmation}, row.HoldFromStatus) {
					return invoiceError(400, "VALIDATION_FAILED")
				}
				description = input.Reason
				row.Status = row.HoldFromStatus
				row.HoldFromStatus = ""
				row.ReasonCipher = ""
			case "confirm":
				if input.DeliveryVersion != row.CurrentDeliveryVersion || row.CurrentDeliveryVersion == 0 {
					return invoiceError(409, "DELIVERY_VERSION_CHANGED")
				}
				row.Status = model.InvoiceConfirmed
				row.ConfirmedAt = now
			case "dispute":
				if input.DeliveryVersion != row.CurrentDeliveryVersion || row.CurrentDeliveryVersion == 0 {
					return invoiceError(409, "DELIVERY_VERSION_CHANGED")
				}
				if !invoiceDescription(input.Description) || !slices.Contains([]string{"title_tax", "amount", "attachment", "incomplete", "other"}, input.Category) {
					return invoiceError(400, "VALIDATION_FAILED")
				}
				attachmentID, err = s.bindEvidence(tx, scope, row, input.EvidenceAttachmentID)
				if err != nil {
					return err
				}
				publicID, err := invoiceID("id_")
				if err != nil {
					return err
				}
				var round int64
				if err = tx.Model(&model.InvoiceDispute{}).Where("request_id = ?", row.ID).Count(&round).Error; err != nil {
					return err
				}
				d := model.InvoiceDispute{PublicID: publicID, TenantID: row.TenantID, RequestID: row.ID, DeliveryVersion: row.CurrentDeliveryVersion, Round: round + 1, Category: input.Category, EvidenceAttachmentID: attachmentID, Status: "open", CreatedAt: now, UpdatedAt: now}
				d.DescriptionCipher, err = invoiceEncrypt(invoiceAAD(row.TenantID, row.UserID, publicID, "description"), input.Description)
				if err != nil {
					return err
				}
				if err = tx.Create(&d).Error; err != nil {
					return err
				}
				row.ActiveDisputeID = d.ID
				row.Status = model.InvoiceDisputed
				description = input.Description
			case "message":
				if !invoiceDescription(input.Description) {
					return invoiceError(400, "VALIDATION_FAILED")
				}
				var dispute model.InvoiceDispute
				if err = tx.Where("id = ? AND public_id = ? AND request_id = ? AND status = ?", row.ActiveDisputeID, input.DisputeID, row.ID, "open").First(&dispute).Error; err != nil {
					return err
				}
				attachmentID, err = s.bindEvidence(tx, scope, row, input.EvidenceAttachmentID)
				if err != nil {
					return err
				}
				description = input.Description
			case "reply":
				if !invoiceDescription(input.Message) {
					return invoiceError(400, "VALIDATION_FAILED")
				}
				var dispute model.InvoiceDispute
				if err = model.InvoiceLockForUpdate(tx).Where("id = ? AND request_id = ? AND public_id = ? AND status = ?", row.ActiveDisputeID, row.ID, input.DisputeID, "open").First(&dispute).Error; err != nil {
					return err
				}
				dispute.ReplyCipher, err = invoiceEncrypt(invoiceAAD(row.TenantID, row.UserID, dispute.PublicID, "reply"), input.Message)
				if err != nil {
					return err
				}
				dispute.Status = "replied"
				dispute.ActorID = scope.UserID
				dispute.UpdatedAt = now
				if err = tx.Save(&dispute).Error; err != nil {
					return err
				}
				row.Status = model.InvoiceAwaitingReconfirmation
				description = input.Message
			case "deliver", "reissue":
				attachmentID, err = s.deliver(tx, scope, &row, action, input)
				if err != nil {
					return err
				}
			default:
				return invoiceError(400, "VALIDATION_FAILED")
			}
		}
		row.Version++
		row.UpdatedAt = now
		update := tx.Model(&model.InvoiceRequest{}).Where("id = ? AND version = ?", row.ID, oldVersion).Updates(map[string]any{"status": row.Status, "version": row.Version, "updated_at": now, "reason_cipher": row.ReasonCipher, "hold_from_status": row.HoldFromStatus, "reviewed_at": row.ReviewedAt, "confirmed_at": row.ConfirmedAt, "delivered_at": row.DeliveredAt, "current_delivery_version": row.CurrentDeliveryVersion, "active_dispute_id": row.ActiveDisputeID, "ever_delivered": row.EverDelivered, "issued_evidence": row.IssuedEvidence})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return invoiceError(409, "VERSION_CONFLICT")
		}
		kind := action
		if committedCode != "" {
			kind = "hold"
		}
		if err = s.event(tx, scope, row, kind, description, attachmentID); err != nil {
			return err
		}
		return invoiceFinishOperation(tx, operation, id, committedCode)
	})
	if err != nil {
		return InvoiceDTO{}, err
	}
	if committedCode != "" {
		return InvoiceDTO{}, invoiceError(409, committedCode)
	}
	return s.Get(ctx, scope, id)
}
func (s InvoiceService) deliver(tx *gorm.DB, scope InvoiceScope, row *model.InvoiceRequest, action string, input InvoiceActionInput) (int64, error) {
	now := time.Now().Unix()
	if !s.Policy().Enabled {
		return 0, invoiceError(403, "FEATURE_DISABLED")
	}
	if input.AmountMinor != strconv.FormatInt(row.AmountMinor, 10) || input.Currency != row.Currency || !invoiceDescription(input.InvoiceNo) || len(input.InvoiceNo) > 128 || strings.ContainsAny(input.InvoiceNo, "\r\n") {
		return 0, invoiceError(422, "INVOICE_METADATA_MISMATCH")
	}
	date, err := time.Parse("2006-01-02", input.IssuedOn)
	location, e := time.LoadLocation(s.Policy().Timezone)
	if e != nil {
		return 0, invoiceError(403, "POLICY_UNCONFIGURED")
	}
	if err != nil || date.Format("2006-01-02") > time.Now().In(location).Format("2006-01-02") {
		return 0, invoiceError(400, "VALIDATION_FAILED")
	}
	var tax int64
	if input.TaxMinor != "" {
		tax, err = strconv.ParseInt(input.TaxMinor, 10, 64)
		if err != nil || tax < 0 || tax > row.AmountMinor {
			return 0, invoiceError(422, "INVOICE_METADATA_MISMATCH")
		}
	}
	var a model.InvoiceAttachment
	if err = model.InvoiceLockForUpdate(tx).Where("tenant_id = ? AND request_id = ? AND public_id = ? AND purpose = ?", row.TenantID, row.ID, input.AttachmentID, "invoice").First(&a).Error; err != nil {
		return 0, err
	}
	if a.Status != "validated" || a.BoundAt != 0 || a.ExpiresAt <= now || a.UploaderID != scope.UserID {
		return 0, invoiceError(409, "ATTACHMENT_ALREADY_BOUND")
	}
	if action == "reissue" {
		var d model.InvoiceDispute
		if err = model.InvoiceLockForUpdate(tx).Where("id = ? AND request_id = ? AND public_id = ? AND delivery_version = ?", row.ActiveDisputeID, row.ID, input.DisputeID, row.CurrentDeliveryVersion).First(&d).Error; err != nil {
			return 0, err
		}
		if !slices.Contains([]string{"open", "replied"}, d.Status) {
			return 0, invoiceError(409, "INVALID_STATE")
		}
		if err = tx.Model(&d).Updates(map[string]any{"status": "reissued", "updated_at": now, "actor_id": scope.UserID}).Error; err != nil {
			return 0, err
		}
	}
	delivery := model.InvoiceDelivery{TenantID: row.TenantID, RequestID: row.ID, DeliveryVersion: row.CurrentDeliveryVersion + 1, AttachmentID: a.ID, InvoiceNo: strings.TrimSpace(input.InvoiceNo), IssuedOn: input.IssuedOn, AmountMinor: row.AmountMinor, Currency: row.Currency, TaxMinor: tax, ActorID: scope.UserID, CreatedAt: now}
	if err = tx.Create(&delivery).Error; err != nil {
		return 0, err
	}
	if err = tx.Model(&a).Update("bound_at", now).Error; err != nil {
		return 0, err
	}
	row.ActiveDisputeID = 0
	row.CurrentDeliveryVersion = delivery.DeliveryVersion
	row.EverDelivered = true
	row.IssuedEvidence = true
	row.Status = model.InvoiceDelivered
	row.DeliveredAt = now
	row.ConfirmedAt = 0
	return a.ID, nil
}
