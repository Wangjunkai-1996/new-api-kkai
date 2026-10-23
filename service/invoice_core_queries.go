package service

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func invoiceRequestQuery(db *gorm.DB, scope InvoiceScope, id string) *gorm.DB {
	q := db.Where("tenant_id = ? AND public_id = ?", scope.TenantID, id)
	if !scope.Admin {
		q = q.Where("user_id = ?", scope.UserID)
	}
	return q
}
func InvoiceAllowedActions(row model.InvoiceRequest, admin bool) []string {
	a := []string{}
	if admin {
		switch row.Status {
		case model.InvoicePendingReview:
			a = append(a, "approve", "reject", "hold")
		case model.InvoiceApprovedForIssue:
			a = append(a, "upload", "deliver", "hold")
		case model.InvoiceDisputed:
			a = append(a, "reply", "reissue", "upload", "hold")
		case model.InvoiceAwaitingReconfirmation:
			a = append(a, "reissue", "upload", "hold")
		case model.InvoiceDelivered, model.InvoiceConfirmed:
			a = append(a, "hold")
		case model.InvoiceFinanceHold:
			a = append(a, "resume", "hold")
			if !row.EverDelivered && !row.IssuedEvidence && slices.Contains([]string{model.InvoicePendingReview, model.InvoiceApprovedForIssue}, row.HoldFromStatus) {
				a = append(a, "reject")
			}
		}
	} else {
		switch row.Status {
		case model.InvoiceDelivered, model.InvoiceAwaitingReconfirmation:
			a = append(a, "confirm", "dispute")
		case model.InvoiceConfirmed:
			a = append(a, "dispute")
		case model.InvoiceDisputed:
			a = append(a, "message")
		case model.InvoiceRejected:
			a = append(a, "reapply")
		}
	}
	return a
}
func invoiceAttachmentDTO(a model.InvoiceAttachment) InvoiceAttachmentDTO {
	return InvoiceAttachmentDTO{ID: a.PublicID, FileName: a.FileName, MIMEType: a.MIMEType, Size: a.Size, SHA256: a.SHA256, ExpiresAt: invoiceTime(a.ExpiresAt)}
}

type invoiceListData struct {
	items     map[int64][]model.InvoiceRequestItem
	usernames map[int]string
}

func (s InvoiceService) requestDTO(db *gorm.DB, row model.InvoiceRequest, admin, detail bool, preloaded ...invoiceListData) (InvoiceDTO, error) {
	dto := InvoiceDTO{PublicID: row.PublicID, DisplayNo: row.PublicID, UserID: row.UserID, Status: row.Status, Version: row.Version, PolicyVersion: row.PolicyVersion, SellerID: row.SellerID, SellerName: row.SellerName, InvoiceType: row.InvoiceType, AmountMinor: strconv.FormatInt(row.AmountMinor, 10), Currency: row.Currency, CurrencyExponent: row.CurrencyExponent, CurrentDeliveryVersion: row.CurrentDeliveryVersion, CreatedAt: invoiceTime(row.CreatedAt), UpdatedAt: invoiceTime(row.UpdatedAt), HoldFromStatus: row.HoldFromStatus, AllowedActions: InvoiceAllowedActions(row, admin), SourceSummary: []string{}, NotificationDeliveryState: "not_requested"}
	aad := invoiceAAD(row.TenantID, row.UserID, row.PublicID, "profile")
	if err := invoiceDecrypt(aad, row.ProfileCipher, &dto.Profile); err != nil {
		return dto, err
	}
	if !detail {
		dto.Profile.Title = invoiceMask(dto.Profile.Title)
		dto.Profile.TaxID = invoiceMask(dto.Profile.TaxID)
		dto.Profile.Email = invoiceMask(dto.Profile.Email)
		dto.Profile.Address = ""
		dto.Profile.Phone = ""
		dto.Profile.BankName = ""
		dto.Profile.BankAccount = ""
	}
	var items []model.InvoiceRequestItem
	if len(preloaded) > 0 {
		items = preloaded[0].items[row.ID]
	} else {
		if err := db.Where("request_id = ? AND tenant_id = ?", row.ID, row.TenantID).Order("id ASC").Find(&items).Error; err != nil {
			return dto, err
		}
	}
	dto.ItemsCount = len(items)
	for _, item := range items {
		if !slices.Contains(dto.SourceSummary, item.SourceType) {
			dto.SourceSummary = append(dto.SourceSummary, item.SourceType)
		}
		if detail {
			amount := strconv.FormatInt(item.AmountMinor, 10)
			dto.Items = append(dto.Items, InvoiceCandidate{InvoiceOrderRef: InvoiceOrderRef{SourceType: item.SourceType, OrderID: item.OrderID}, OrderNo: item.OrderNo, AmountMinor: &amount, Currency: item.Currency, CurrencyExponent: row.CurrencyExponent, PaidAt: invoiceTime(item.PaidAt), PaymentLabel: item.PaymentLabel, ReasonCodes: []string{}, AllowedActions: []string{}, AllowedInvoiceTypes: []string{row.InvoiceType}})
		}
	}
	if admin {
		if len(preloaded) > 0 {
			dto.Username = preloaded[0].usernames[row.UserID]
		} else {
			var user model.User
			err := db.Select("username").Where("id = ? AND tenant_id = ?", row.UserID, row.TenantID).First(&user).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return dto, err
			}
			dto.Username = user.Username
		}
	}
	if !detail {
		return dto, nil
	}
	if err := invoiceDecrypt(invoiceAAD(row.TenantID, row.UserID, row.PublicID, "note"), row.NoteCipher, &dto.Note); err != nil {
		return dto, err
	}
	if row.ReasonCipher != "" {
		if err := invoiceDecrypt(invoiceAAD(row.TenantID, row.UserID, row.PublicID, "reason"), row.ReasonCipher, &dto.Reason); err != nil {
			return dto, err
		}
	}
	var deliveries []model.InvoiceDelivery
	if err := db.Where("request_id = ? AND tenant_id = ?", row.ID, row.TenantID).Order("delivery_version DESC").Find(&deliveries).Error; err != nil {
		return dto, err
	}
	var attachmentRows []model.InvoiceAttachment
	if err := db.Where("request_id = ? AND tenant_id = ? AND bound_at > 0", row.ID, row.TenantID).Find(&attachmentRows).Error; err != nil {
		return dto, err
	}
	attachments := map[int64]model.InvoiceAttachment{}
	for _, a := range attachmentRows {
		attachments[a.ID] = a
	}
	for _, d := range deliveries {
		attachment, ok := attachments[d.AttachmentID]
		if !ok {
			return dto, gorm.ErrRecordNotFound
		}
		v := InvoiceDeliveryDTO{DeliveryVersion: d.DeliveryVersion, File: invoiceAttachmentDTO(attachment), InvoiceNo: d.InvoiceNo, IssuedOn: d.IssuedOn, AmountMinor: strconv.FormatInt(d.AmountMinor, 10), Currency: d.Currency, TaxMinor: strconv.FormatInt(d.TaxMinor, 10), CreatedAt: invoiceTime(d.CreatedAt)}
		dto.DeliveredVersions = append(dto.DeliveredVersions, v)
		if d.DeliveryVersion == row.CurrentDeliveryVersion {
			current := v
			dto.CurrentDelivery = &current
		}
	}
	if row.ActiveDisputeID > 0 {
		var dispute model.InvoiceDispute
		if err := db.Where("id = ? AND request_id = ? AND tenant_id = ?", row.ActiveDisputeID, row.ID, row.TenantID).First(&dispute).Error; err != nil {
			return dto, err
		}
		d := InvoiceDisputeDTO{ID: dispute.PublicID, DeliveryVersion: dispute.DeliveryVersion, Round: dispute.Round, Category: dispute.Category, Status: dispute.Status, CreatedAt: invoiceTime(dispute.CreatedAt)}
		if err := invoiceDecrypt(invoiceAAD(row.TenantID, row.UserID, dispute.PublicID, "description"), dispute.DescriptionCipher, &d.Description); err != nil {
			return dto, err
		}
		if dispute.ReplyCipher != "" {
			if err := invoiceDecrypt(invoiceAAD(row.TenantID, row.UserID, dispute.PublicID, "reply"), dispute.ReplyCipher, &d.Reply); err != nil {
				return dto, err
			}
		}
		if dispute.EvidenceAttachmentID > 0 {
			a, ok := attachments[dispute.EvidenceAttachmentID]
			if !ok {
				return dto, gorm.ErrRecordNotFound
			}
			att := invoiceAttachmentDTO(a)
			d.Evidence = &att
		}
		dto.ActiveDispute = &d
	}
	var events []model.InvoiceEvent
	if err := db.Where("request_id = ? AND tenant_id = ?", row.ID, row.TenantID).Order("id ASC").Find(&events).Error; err != nil {
		return dto, err
	}
	for _, e := range events {
		event, err := invoiceEventDTO(e, row)
		if err != nil {
			return dto, err
		}
		if e.AttachmentID > 0 {
			a, ok := attachments[e.AttachmentID]
			if !ok {
				return dto, gorm.ErrRecordNotFound
			}
			att := invoiceAttachmentDTO(a)
			event.Attachment = &att
		}
		dto.Events = append(dto.Events, event)
	}
	// Only administrator/system progress events send mail. A later customer
	// confirmation must not hide an earlier failed delivery notification.
	for i := len(events) - 1; i >= 0; i-- {
		if !events[i].ActorAdmin {
			continue
		}
		if common.SMTPServer == "" {
			dto.NotificationDeliveryState = "not_configured"
			break
		}
		var mail model.KKAIOutboxEvent
		err := db.Where("topic = ? AND event_key = ?", InvoiceNotificationTopic, "invoice:event:"+events[i].PublicID).First(&mail).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return dto, err
		}
		if err == nil {
			switch mail.Status {
			case model.KKAIOutboxStatusDelivered:
				dto.NotificationDeliveryState = "sent"
			case model.KKAIOutboxStatusDead:
				dto.NotificationDeliveryState = "failed"
			case model.KKAIOutboxStatusPending:
				dto.NotificationDeliveryState = "queued"
				if mail.Attempts > 0 {
					dto.NotificationDeliveryState = "retrying"
				}
			}
		}
		break
	}
	return dto, nil
}
func invoiceEventDTO(e model.InvoiceEvent, row model.InvoiceRequest) (InvoiceEventDTO, error) {
	dto := InvoiceEventDTO{ID: e.PublicID, Kind: e.Kind, Status: e.Status, ActorAdmin: e.ActorAdmin, DeliveryVersion: e.DeliveryVersion, CreatedAt: invoiceTime(e.CreatedAt)}
	if e.DescriptionCipher != "" {
		if err := invoiceDecrypt(invoiceAAD(row.TenantID, row.UserID, e.PublicID, "event"), e.DescriptionCipher, &dto.Description); err != nil {
			return dto, err
		}
	}
	return dto, nil
}
func (s InvoiceService) Get(ctx context.Context, scope InvoiceScope, id string) (InvoiceDTO, error) {
	if err := invoiceValidateScope(scope); err != nil {
		return InvoiceDTO{}, err
	}
	var row model.InvoiceRequest
	db := s.database(ctx)
	if err := invoiceRequestQuery(db, scope, id).First(&row).Error; err != nil {
		return InvoiceDTO{}, err
	}
	return s.requestDTO(db, row, scope.Admin, true)
}
func invoiceListQuery(db *gorm.DB, scope InvoiceScope, f InvoiceFilter, groups bool) (*gorm.DB, error) {
	q := db.Model(&model.InvoiceRequest{}).Where("invoice_requests.tenant_id = ?", scope.TenantID)
	if !scope.Admin {
		q = q.Where("invoice_requests.user_id = ?", scope.UserID)
	}
	statuses := []string{model.InvoicePendingReview, model.InvoiceApprovedForIssue, model.InvoiceDelivered, model.InvoiceConfirmed, model.InvoiceDisputed, model.InvoiceAwaitingReconfirmation, model.InvoiceFinanceHold, model.InvoiceRejected}
	if groups {
		if f.Status != "" {
			if !slices.Contains(statuses, f.Status) {
				return nil, invoiceError(400, "VALIDATION_FAILED")
			}
			q = q.Where("invoice_requests.status = ?", f.Status)
		}
		if scope.Admin {
			if f.Queue != "" && f.Queue != "all" {
				if !slices.Contains([]string{model.InvoicePendingReview, model.InvoiceApprovedForIssue, model.InvoiceDisputed}, f.Queue) {
					return nil, invoiceError(400, "VALIDATION_FAILED")
				}
				q = q.Where("invoice_requests.status = ?", f.Queue)
			}
		} else {
			switch f.Group {
			case "", "all":
			case "processing":
				q = q.Where("invoice_requests.status IN ?", []string{model.InvoicePendingReview, model.InvoiceApprovedForIssue, model.InvoiceDisputed, model.InvoiceFinanceHold})
			case "awaiting_confirmation":
				q = q.Where("invoice_requests.status IN ?", []string{model.InvoiceDelivered, model.InvoiceAwaitingReconfirmation})
			case "completed":
				q = q.Where("invoice_requests.status IN ?", []string{model.InvoiceConfirmed, model.InvoiceRejected})
			default:
				return nil, invoiceError(400, "VALIDATION_FAILED")
			}
		}
	}
	if f.Currency != "" {
		q = q.Where("invoice_requests.currency = ?", f.Currency)
	}
	if f.From != "" {
		d, _ := time.Parse(time.RFC3339, f.From)
		q = q.Where("invoice_requests.created_at >= ?", d.Unix())
	}
	if f.To != "" {
		d, _ := time.Parse(time.RFC3339, f.To)
		q = q.Where("invoice_requests.created_at < ?", d.Unix())
	}
	if f.SourceType != "" {
		q = q.Where("invoice_requests.id IN (?)", db.Model(&model.InvoiceRequestItem{}).Select("request_id").Where("tenant_id = ? AND source_type = ?", scope.TenantID, f.SourceType))
	}
	if f.Q != "" {
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.Q) + "%"
		items := db.Model(&model.InvoiceRequestItem{}).Select("request_id").Where("tenant_id = ? AND order_no LIKE ? ESCAPE '!'", scope.TenantID, pattern)
		if scope.Admin {
			users := db.Model(&model.User{}).Select("id").Where("tenant_id = ? AND (username LIKE ? ESCAPE '!' OR email LIKE ? ESCAPE '!')", scope.TenantID, pattern, pattern)
			q = q.Where("invoice_requests.public_id LIKE ? ESCAPE '!' OR invoice_requests.id IN (?) OR invoice_requests.user_id IN (?)", pattern, items, users)
		} else {
			q = q.Where("invoice_requests.public_id LIKE ? ESCAPE '!' OR invoice_requests.id IN (?)", pattern, items)
		}
	}
	return q, nil
}
func (s InvoiceService) List(ctx context.Context, scope InvoiceScope, f InvoiceFilter) (InvoicePage[InvoiceDTO], error) {
	result := InvoicePage[InvoiceDTO]{Items: []InvoiceDTO{}}
	if err := invoiceValidateScope(scope); err != nil {
		return result, err
	}
	if err := invoicePaging(&f); err != nil {
		return result, err
	}
	result.P, result.PageSize = f.P, f.PageSize
	db := s.database(ctx)
	q, err := invoiceListQuery(db, scope, f, true)
	if err != nil {
		return result, err
	}
	if err = q.Count(&result.Total).Error; err != nil {
		return result, err
	}
	var rows []model.InvoiceRequest
	if err = q.Order("invoice_requests.created_at DESC, invoice_requests.id DESC").Offset((f.P - 1) * f.PageSize).Limit(f.PageSize).Find(&rows).Error; err != nil {
		return result, err
	}
	data := invoiceListData{items: map[int64][]model.InvoiceRequestItem{}, usernames: map[int]string{}}
	if len(rows) > 0 {
		ids := []int64{}
		users := []int{}
		for _, row := range rows {
			ids = append(ids, row.ID)
			users = append(users, row.UserID)
		}
		var allItems []model.InvoiceRequestItem
		if err := db.Where("tenant_id = ? AND request_id IN ?", scope.TenantID, ids).Order("id ASC").Find(&allItems).Error; err != nil {
			return result, err
		}
		for _, item := range allItems {
			data.items[item.RequestID] = append(data.items[item.RequestID], item)
		}
		if scope.Admin {
			var allUsers []model.User
			if err := db.Select("id", "username").Where("tenant_id = ? AND id IN ?", scope.TenantID, users).Find(&allUsers).Error; err != nil {
				return result, err
			}
			for _, user := range allUsers {
				data.usernames[user.Id] = user.Username
			}
		}
	}
	for _, row := range rows {
		dto, err := s.requestDTO(db, row, scope.Admin, false, data)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, dto)
	}
	return result, nil
}
func (s InvoiceService) Summary(ctx context.Context, scope InvoiceScope, f InvoiceFilter) (InvoiceSummary, error) {
	result := InvoiceSummary{Totals: []InvoiceMoneyTotal{}, Counts: map[string]int64{}}
	if err := invoiceValidateScope(scope); err != nil {
		return result, err
	}
	if err := invoicePaging(&f); err != nil {
		return result, err
	}
	db := s.database(ctx)
	q, err := invoiceListQuery(db, scope, f, false)
	if err != nil {
		return result, err
	}
	var counts []struct {
		Status string
		Count  int64
	}
	if err = q.Select("status, COUNT(*) AS count").Group("status").Find(&counts).Error; err != nil {
		return result, err
	}
	for _, c := range counts {
		result.Counts[c.Status] = c.Count
		result.All += c.Count
		switch c.Status {
		case model.InvoicePendingReview:
			result.PendingReview = c.Count
		case model.InvoiceApprovedForIssue:
			result.ApprovedForIssue = c.Count
		case model.InvoiceDisputed:
			result.Disputed = c.Count
		case model.InvoiceFinanceHold:
			result.FinanceHold = c.Count
		}
		if slices.Contains([]string{model.InvoicePendingReview, model.InvoiceApprovedForIssue, model.InvoiceDisputed, model.InvoiceFinanceHold}, c.Status) {
			result.ProcessingCount += c.Count
		}
		if slices.Contains([]string{model.InvoiceDelivered, model.InvoiceAwaitingReconfirmation}, c.Status) {
			result.AwaitingConfirmationCount += c.Count
		}
	}
	if !scope.Admin {
		rows, err := s.candidateRows(ctx, scope, db, false)
		if err != nil {
			return result, err
		}
		var total int64
		for _, c := range rows {
			if c.Eligible {
				result.EligibleCount++
				if total > 9223372036854775807-c.amount {
					return result, invoiceError(422, "INVOICE_METADATA_MISMATCH")
				}
				total += c.amount
			}
		}
		result.Totals = []InvoiceMoneyTotal{{Currency: "CNY", CurrencyExponent: 2, AmountMinor: strconv.FormatInt(total, 10), Count: result.EligibleCount}}
	}
	_, _, unread, err := s.notificationRows(ctx, scope, InvoiceFilter{P: 1, PageSize: 1})
	result.UnreadCount = unread
	return result, err
}
func (s InvoiceService) notificationRows(ctx context.Context, scope InvoiceScope, f InvoiceFilter) ([]model.InvoiceEvent, int64, int64, error) {
	q := s.database(ctx).Model(&model.InvoiceEvent{}).Where("invoice_events.tenant_id = ? AND invoice_events.actor_id <> ?", scope.TenantID, scope.UserID)
	if !scope.Admin {
		q = q.Where("invoice_events.user_id = ? AND invoice_events.actor_admin = ?", scope.UserID, true)
	} else {
		q = q.Where("invoice_events.actor_admin = ? OR invoice_events.actor_id = ?", false, 0)
	}
	reads := s.database(ctx).Model(&model.InvoiceEventRead{}).Select("event_id").Where("tenant_id = ? AND user_id = ?", scope.TenantID, scope.UserID)
	var total, unread int64
	if err := q.Session(&gorm.Session{}).Where("invoice_events.id NOT IN (?)", reads).Count(&unread).Error; err != nil {
		return nil, 0, 0, err
	}
	if f.UnreadOnly {
		q = q.Where("invoice_events.id NOT IN (?)", reads)
	}
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	var rows []model.InvoiceEvent
	err := q.Order("id DESC").Offset((f.P - 1) * f.PageSize).Limit(f.PageSize).Find(&rows).Error
	return rows, total, unread, err
}
func (s InvoiceService) Notifications(ctx context.Context, scope InvoiceScope, f InvoiceFilter) (InvoiceNotificationPage, error) {
	result := InvoiceNotificationPage{InvoicePage: InvoicePage[InvoiceNotification]{Items: []InvoiceNotification{}}}
	if err := invoiceValidateScope(scope); err != nil {
		return result, err
	}
	if err := invoicePaging(&f); err != nil {
		return result, err
	}
	result.P, result.PageSize = f.P, f.PageSize
	rows, total, unread, err := s.notificationRows(ctx, scope, f)
	if err != nil {
		return result, err
	}
	result.Total, result.UnreadCount = total, unread
	if len(rows) == 0 {
		return result, nil
	}
	db := s.database(ctx)
	requestIDs := make([]int64, 0, len(rows))
	eventIDs := make([]int64, 0, len(rows))
	for _, e := range rows {
		requestIDs = append(requestIDs, e.RequestID)
		eventIDs = append(eventIDs, e.ID)
	}
	var requests []model.InvoiceRequest
	if err := db.Select("id", "public_id").Where("tenant_id = ? AND id IN ?", scope.TenantID, requestIDs).Find(&requests).Error; err != nil {
		return result, err
	}
	publicIDs := make(map[int64]string, len(requests))
	for _, request := range requests {
		publicIDs[request.ID] = request.PublicID
	}
	var reads []model.InvoiceEventRead
	if err := db.Where("tenant_id = ? AND user_id = ? AND event_id IN ?", scope.TenantID, scope.UserID, eventIDs).Find(&reads).Error; err != nil {
		return result, err
	}
	readAt := make(map[int64]string, len(reads))
	for _, read := range reads {
		readAt[read.EventID] = invoiceTime(read.ReadAt)
	}
	for _, e := range rows {
		publicID, ok := publicIDs[e.RequestID]
		if !ok {
			return result, gorm.ErrRecordNotFound
		}
		// Notification summaries never expose or need to decrypt event descriptions.
		dto := InvoiceEventDTO{ID: e.PublicID, Kind: e.Kind, Status: e.Status, ActorAdmin: e.ActorAdmin, DeliveryVersion: e.DeliveryVersion, CreatedAt: invoiceTime(e.CreatedAt)}
		result.Items = append(result.Items, InvoiceNotification{InvoiceEventDTO: dto, RequestPublicID: publicID, ReadAt: readAt[e.ID]})
	}
	return result, nil
}
func (s InvoiceService) ReadNotifications(ctx context.Context, scope InvoiceScope, eventID string) error {
	if err := invoiceValidateScope(scope); err != nil {
		return err
	}
	db := s.database(ctx)
	q := db.Where("tenant_id = ? AND actor_id <> ?", scope.TenantID, scope.UserID)
	if !scope.Admin {
		q = q.Where("user_id = ? AND actor_admin = ?", scope.UserID, true)
	} else {
		q = q.Where("actor_admin = ? OR actor_id = ?", false, 0)
	}
	if eventID != "" {
		q = q.Where("public_id = ?", eventID)
	} else {
		reads := db.Model(&model.InvoiceEventRead{}).Select("event_id").Where("tenant_id = ? AND user_id = ?", scope.TenantID, scope.UserID)
		q = q.Where("id NOT IN (?)", reads)
	}
	var rows []model.InvoiceEvent
	if err := q.Select("id").Find(&rows).Error; err != nil {
		return err
	}
	if eventID != "" && len(rows) == 0 {
		return gorm.ErrRecordNotFound
	}
	if len(rows) == 0 {
		return nil
	}
	reads := make([]model.InvoiceEventRead, 0, len(rows))
	now := time.Now().Unix()
	for _, e := range rows {
		reads = append(reads, model.InvoiceEventRead{TenantID: scope.TenantID, EventID: e.ID, UserID: scope.UserID, ReadAt: now})
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(reads, 100).Error
}
