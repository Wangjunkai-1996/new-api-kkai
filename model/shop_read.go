package model

import "gorm.io/gorm"

// ShopRecordFilter is for explicit admin lists; filters are applied before count.
type ShopRecordFilter struct {
	Page            int
	Limit           int
	ProductPublicID string
	OrderPublicID   string
	ReceiptPublicID string
	State           string
	Kind            string
	BatchPublicID   string
}

func shopReadScope(db *gorm.DB, scope ShopScope, admin bool) error {
	return shopAuthorize(db, ShopCommand{ActorID: scope.UserID, TenantID: scope.TenantID, Admin: scope.Admin}, admin)
}
func AuthorizeShopAdmin(db *gorm.DB, scope ShopScope) error {
	return shopReadScope(db, scope, true)
}
func shopReadPage(q *gorm.DB, page, limit int, dest any) (int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if page > 1000000 {
		return 0, ErrShopInvalid
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, q.Order("id desc").Offset((page - 1) * limit).Limit(limit).Find(dest).Error
}
func shopReceiptListQuery(db *gorm.DB, f ShopRecordFilter) *gorm.DB {
	q := db.Model(&ShopPaymentReceipt{})
	if f.OrderPublicID != "" {
		q = q.Where("order_id IN (?)", db.Model(&ShopOrder{}).Select("id").Where("public_id = ? AND tenant_id = ?", f.OrderPublicID, PlatformTenantID))
	}
	if f.State != "" {
		q = q.Where("processing_state = ?", f.State)
	}
	return q
}
func GetShopInventory(db *gorm.DB, scope ShopScope, publicID string) (*ShopStockUnit, error) {
	if err := shopReadScope(db, scope, true); err != nil {
		return nil, err
	}
	var row ShopStockUnit
	err := db.Omit("secret_ciphertext", "secret_key_version", "fingerprint_key_version", "identity_fingerprint", "identity_domain_hash").Where("public_id = ? AND tenant_id = ?", publicID, PlatformTenantID).First(&row).Error
	return &row, shopNotFound(err)
}
func ListShopInventory(db *gorm.DB, scope ShopScope, f ShopRecordFilter) ([]ShopStockUnit, int64, error) {
	if err := shopReadScope(db, scope, true); err != nil {
		return nil, 0, err
	}
	q := db.Model(&ShopStockUnit{}).Omit("secret_ciphertext", "secret_key_version", "fingerprint_key_version", "identity_fingerprint", "identity_domain_hash").Where("tenant_id = ?", PlatformTenantID)
	if f.ProductPublicID != "" {
		q = q.Where("product_id IN (?)", db.Model(&ShopProduct{}).Select("id").Where("public_id = ? AND tenant_id = ?", f.ProductPublicID, PlatformTenantID))
	}
	if f.State != "" {
		q = q.Where("state = ?", f.State)
	}
	if f.BatchPublicID != "" {
		q = q.Where("import_batch_public_id = ?", f.BatchPublicID)
	}
	var rows []ShopStockUnit
	count, err := shopReadPage(q, f.Page, f.Limit, &rows)
	return rows, count, err
}
func GetShopPaymentReceipt(db *gorm.DB, scope ShopScope, publicID string) (*ShopPaymentReceipt, error) {
	if err := shopReadScope(db, scope, true); err != nil {
		return nil, err
	}
	var row ShopPaymentReceipt
	err := db.Where("public_id = ?", publicID).First(&row).Error
	return &row, shopNotFound(err)
}
func ListShopPaymentReceipts(db *gorm.DB, scope ShopScope, f ShopRecordFilter) ([]ShopPaymentReceipt, int64, error) {
	if err := shopReadScope(db, scope, true); err != nil {
		return nil, 0, err
	}
	var rows []ShopPaymentReceipt
	n, err := shopReadPage(shopReceiptListQuery(db, f), f.Page, f.Limit, &rows)
	return rows, n, err
}
func GetShopRefund(db *gorm.DB, scope ShopScope, publicID string) (*ShopRefundCase, error) {
	if err := shopReadScope(db, scope, true); err != nil {
		return nil, err
	}
	var row ShopRefundCase
	err := db.Where("public_id = ?", publicID).First(&row).Error
	return &row, shopNotFound(err)
}
func ListShopRefunds(db *gorm.DB, scope ShopScope, f ShopRecordFilter) ([]ShopRefundCase, int64, error) {
	if err := shopReadScope(db, scope, true); err != nil {
		return nil, 0, err
	}
	q := db.Model(&ShopRefundCase{})
	if f.ReceiptPublicID != "" {
		q = q.Where("receipt_id IN (?)", db.Model(&ShopPaymentReceipt{}).Select("id").Where("public_id = ?", f.ReceiptPublicID))
	}
	if f.OrderPublicID != "" {
		q = q.Where("order_id IN (?)", db.Model(&ShopOrder{}).Select("id").Where("public_id = ? AND tenant_id = ?", f.OrderPublicID, PlatformTenantID))
	}
	if f.State != "" {
		q = q.Where("state = ?", f.State)
	}
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	var rows []ShopRefundCase
	n, err := shopReadPage(q, f.Page, f.Limit, &rows)
	return rows, n, err
}
func ListShopRefundAttempts(db *gorm.DB, scope ShopScope, refundPublicID string, page, limit int) ([]ShopRefundAttempt, int64, error) {
	if err := shopReadScope(db, scope, true); err != nil {
		return nil, 0, err
	}
	refund, err := GetShopRefund(db, scope, refundPublicID)
	if err != nil {
		return nil, 0, err
	}
	var rows []ShopRefundAttempt
	n, err := shopReadPage(db.Model(&ShopRefundAttempt{}).Where("refund_case_id = ?", refund.ID), page, limit, &rows)
	return rows, n, err
}
func GetShopPaymentEvidence(db *gorm.DB, scope ShopScope, publicID string) (*ShopPaymentEvidence, error) {
	if err := shopReadScope(db, scope, true); err != nil {
		return nil, err
	}
	var row ShopPaymentEvidence
	err := db.Where("public_id = ?", publicID).First(&row).Error
	return &row, shopNotFound(err)
}
func ListShopPaymentEvidence(db *gorm.DB, scope ShopScope, receiptPublicID string, page, limit int) ([]ShopPaymentEvidence, int64, error) {
	receipt, err := GetShopPaymentReceipt(db, scope, receiptPublicID)
	if err != nil {
		return nil, 0, err
	}
	var rows []ShopPaymentEvidence
	n, err := shopReadPage(db.Model(&ShopPaymentEvidence{}).Where("receipt_id = ?", receipt.ID), page, limit, &rows)
	return rows, n, err
}
func GetShopPaymentAdjustment(db *gorm.DB, scope ShopScope, publicID string) (*ShopPaymentAdjustment, error) {
	if err := shopReadScope(db, scope, true); err != nil {
		return nil, err
	}
	var row ShopPaymentAdjustment
	err := db.Where("public_id = ?", publicID).First(&row).Error
	return &row, shopNotFound(err)
}
func ListShopPaymentAdjustments(db *gorm.DB, scope ShopScope, receiptPublicID string, page, limit int) ([]ShopPaymentAdjustment, int64, error) {
	receipt, err := GetShopPaymentReceipt(db, scope, receiptPublicID)
	if err != nil {
		return nil, 0, err
	}
	var rows []ShopPaymentAdjustment
	n, err := shopReadPage(db.Model(&ShopPaymentAdjustment{}).Where("receipt_id = ?", receipt.ID), page, limit, &rows)
	return rows, n, err
}
func ListShopAftersaleMessages(db *gorm.DB, scope ShopScope, aftersalePublicID string, page, limit int) ([]ShopEvent, int64, error) {
	if err := shopReadScope(db, scope, scope.Admin); err != nil {
		return nil, 0, err
	}
	row, err := GetShopAftersale(db, scope, aftersalePublicID)
	if err != nil {
		return nil, 0, err
	}
	q := db.Model(&ShopEvent{}).Where("aftersale_id = ?", row.ID)
	if !scope.Admin {
		q = q.Where("visibility = ?", "public")
	}
	var rows []ShopEvent
	n, err := shopReadPage(q, page, limit, &rows)
	return rows, n, err
}
func ListShopOrderEvents(db *gorm.DB, scope ShopScope, orderPublicID string, page, limit int) ([]ShopEvent, int64, error) {
	if err := shopReadScope(db, scope, scope.Admin); err != nil {
		return nil, 0, err
	}
	row, err := GetShopOrder(db, scope, orderPublicID)
	if err != nil {
		return nil, 0, err
	}
	q := db.Model(&ShopEvent{}).Where("order_id = ?", row.ID)
	if !scope.Admin {
		q = q.Where("visibility = ?", "public")
	}
	var rows []ShopEvent
	n, err := shopReadPage(q, page, limit, &rows)
	return rows, n, err
}

// GetShopRefundableAmounts returns the currently uncommitted whole receipt per
// eligible refund kind. Existing noncancelled cases reserve that responsibility.
func GetShopRefundableAmounts(db *gorm.DB, scope ShopScope, receiptPublicID string) (map[string]int64, error) {
	if err := shopReadScope(db, scope, true); err != nil {
		return nil, err
	}
	amounts := map[string]int64{"goods": 0, "extra": 0, "mismatch": 0}
	var receipt ShopPaymentReceipt
	if err := db.Where("public_id = ?", receiptPublicID).First(&receipt).Error; err != nil {
		return nil, shopNotFound(err)
	}
	adjusted, pending, err := shopFundsAdjusted(db, &receipt, "")
	if err != nil {
		return nil, err
	}
	var cases int64
	if err = db.Model(&ShopRefundCase{}).Where("receipt_id = ? AND state <> ?", receipt.ID, "cancelled").Count(&cases).Error; err != nil {
		return nil, err
	}
	if receipt.AmountMinor <= 0 || adjusted != 0 || pending || cases > 0 {
		return amounts, nil
	}
	if receipt.ProcessingState == "extra" {
		amounts["extra"] = receipt.AmountMinor
	}
	if receipt.ProcessingState == "review" {
		amounts["mismatch"] = receipt.AmountMinor
	}
	if receipt.ProcessingState == "matched" && receipt.OrderID != nil {
		var order ShopOrder
		if err = db.First(&order, *receipt.OrderID).Error; err != nil {
			return nil, err
		}
		if order.EffectiveReceiptID != nil && *order.EffectiveReceiptID == receipt.ID {
			amounts["goods"] = receipt.AmountMinor
		}
	}
	return amounts, nil
}

// ListShopOrderSummaries loads only current facts needed for list projections,
// with a bounded number of queries independent of the number of orders.
func ListShopOrderSummaries(db *gorm.DB, scope ShopScope, filter ShopOrderFilter) ([]ShopOrderDetail, int64, error) {
	orders, count, err := ListShopOrders(db, scope, filter)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]ShopOrderDetail, len(orders))
	if len(orders) == 0 {
		return rows, count, nil
	}
	orderIDs, deliveryIDs, aftersaleIDs, receiptIDs, attemptIDs := []int64{}, []int64{}, []int64{}, []int64{}, []int64{}
	indices := make(map[int64]int, len(orders))
	for i, order := range orders {
		rows[i].Order = order
		if rows[i].Snapshot, err = shopSnapshot(&order); err != nil {
			return nil, 0, err
		}
		indices[order.ID] = i
		orderIDs = append(orderIDs, order.ID)
		if order.CurrentDeliveryID != nil {
			deliveryIDs = append(deliveryIDs, *order.CurrentDeliveryID)
		}
		if order.ActiveAftersaleID != nil {
			aftersaleIDs = append(aftersaleIDs, *order.ActiveAftersaleID)
		}
		if order.EffectiveReceiptID != nil {
			receiptIDs = append(receiptIDs, *order.EffectiveReceiptID)
		}
		if order.ActiveAttemptID != nil {
			attemptIDs = append(attemptIDs, *order.ActiveAttemptID)
		}
	}
	var deliveries []ShopDelivery
	var aftersales []ShopAftersale
	var receipts []ShopPaymentReceipt
	var attempts []ShopPaymentAttempt
	var refunds []ShopRefundCase
	for _, item := range []struct {
		ids  []int64
		dest any
	}{{deliveryIDs, &deliveries}, {aftersaleIDs, &aftersales}, {receiptIDs, &receipts}, {attemptIDs, &attempts}} {
		if len(item.ids) > 0 {
			if err = db.Where("id IN ? AND order_id IN ?", item.ids, orderIDs).Find(item.dest).Error; err != nil {
				return nil, 0, err
			}
		}
	}
	if len(receiptIDs) > 0 {
		if err = db.Where("receipt_id IN ? AND order_id IN ? AND kind = ?", receiptIDs, orderIDs, "goods").Find(&refunds).Error; err != nil {
			return nil, 0, err
		}
		var blockedReceiptIDs []int64
		if err = db.Model(&ShopPaymentAdjustment{}).Distinct("receipt_id").Where("receipt_id IN ? AND state IN ?", receiptIDs, []string{"pending", "succeeded", "disputed"}).Pluck("receipt_id", &blockedReceiptIDs).Error; err != nil {
			return nil, 0, err
		}
		blocked := make(map[int64]bool, len(blockedReceiptIDs))
		for _, id := range blockedReceiptIDs {
			blocked[id] = true
		}
		for i := range rows {
			if rows[i].Order.EffectiveReceiptID != nil {
				rows[i].HasPaymentAdjustment = blocked[*rows[i].Order.EffectiveReceiptID]
			}
		}
	}
	for _, delivery := range deliveries {
		i := indices[delivery.OrderID]
		rows[i].Deliveries = []ShopDelivery{delivery}
		rows[i].CurrentDelivery = &delivery
	}
	for _, aftersale := range aftersales {
		rows[indices[aftersale.OrderID]].Aftersales = []ShopAftersale{aftersale}
	}
	for _, receipt := range receipts {
		if receipt.OrderID != nil {
			rows[indices[*receipt.OrderID]].Receipts = []ShopPaymentReceipt{receipt}
		}
	}
	for _, attempt := range attempts {
		rows[indices[attempt.OrderID]].Attempts = []ShopPaymentAttempt{attempt}
	}
	for _, refund := range refunds {
		if refund.OrderID != nil {
			i := indices[*refund.OrderID]
			rows[i].Refunds = append(rows[i].Refunds, refund)
		}
	}
	return rows, count, nil
}

// ShopAdminOrderActions projects the state gates of MutateShopOrder. Each write
// independently validates authorization, version, reason and evidence.
func ShopAdminOrderActions(detail *ShopOrderDetail) []string {
	if detail == nil {
		return []string{}
	}
	order, snapshot := detail.Order, detail.Snapshot
	actions := []string{"claim-order", "order-note", "risk-hold", "risk-release"}
	if snapshot.DeliveryType == "manual" && order.PaymentState == "paid" && order.DeliveryState != "delivered" {
		for _, field := range snapshot.Terms.BuyerInputSchema {
			if field.Required {
				actions = append(actions, "request-input")
				break
			}
		}
	}
	if snapshot.DeliveryType == "automatic" && order.DeliveryState == "exception" && order.CurrentDeliveryID == nil && !ShopOrderDeliveryBlocked(detail) {
		actions = append(actions, "retry-delivery")
	}
	return actions
}

// ShopOrderDeliveryBlocked is the read projection of the independent funds
// fence used by delivery/reveal writes. It does not grant access by itself.
func ShopOrderDeliveryBlocked(detail *ShopOrderDetail) bool {
	if detail == nil {
		return true
	}
	order := detail.Order
	if order.PaymentState != "paid" || order.EffectiveReceiptID == nil || order.RiskHold || detail.HasPaymentAdjustment {
		return true
	}
	matched := false
	for _, receipt := range detail.Receipts {
		if receipt.ID == *order.EffectiveReceiptID && receipt.ProcessingState == "matched" && receipt.OrderID != nil && *receipt.OrderID == order.ID {
			matched = true
			break
		}
	}
	if !matched {
		return true
	}
	for _, refund := range detail.Refunds {
		if refund.ReceiptID == *order.EffectiveReceiptID && refund.State != "cancelled" {
			return true
		}
	}
	return false
}
