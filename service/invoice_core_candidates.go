package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func invoiceHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func invoicePaging(filter *InvoiceFilter) error {
	if filter.P == 0 {
		filter.P = 1
	}
	if filter.PageSize == 0 {
		filter.PageSize = 20
	}
	if filter.P < 1 || filter.P > 100000 || filter.PageSize < 1 || filter.PageSize > 100 || len(filter.Q) > 128 {
		return invoiceError(400, "VALIDATION_FAILED")
	}
	filter.Q = strings.TrimSpace(filter.Q)
	for _, date := range []string{filter.From, filter.To} {
		if date != "" {
			if _, err := time.Parse(time.RFC3339, date); err != nil {
				return invoiceError(400, "VALIDATION_FAILED")
			}
		}
	}
	return nil
}
func invoiceDateMatches(at int64, f InvoiceFilter) bool {
	if f.From != "" {
		from, _ := time.Parse(time.RFC3339, f.From)
		if at < from.Unix() {
			return false
		}
	}
	if f.To != "" {
		to, _ := time.Parse(time.RFC3339, f.To)
		if at >= to.Unix() {
			return false
		}
	}
	return true
}

// ponytail: merge one user's payment history in memory; use a SQL union projection if individual histories outgrow memory.
func (s InvoiceService) candidateRows(ctx context.Context, scope InvoiceScope, db *gorm.DB, lock bool) ([]InvoiceCandidate, error) {
	return s.candidateRowsFor(ctx, scope, db, lock, nil, false)
}

type invoicePaymentSnapshot struct {
	TopUps        []model.TopUp
	TopUpReceipts []model.InvoicePaymentReceipt
	Memberships   []model.MembershipOrder
	Receipts      []model.MembershipPaymentReceipt
}

func (s InvoiceService) candidateRowsFor(ctx context.Context, scope InvoiceScope, db *gorm.DB, lock bool, refs []InvoiceOrderRef, factsOnly bool, locked ...*invoicePaymentSnapshot) ([]InvoiceCandidate, error) {
	if err := invoiceValidateScope(scope); err != nil {
		return nil, err
	}
	p := s.Policy()
	var topups []model.TopUp
	var memberships []model.MembershipOrder
	var legacy []model.SubscriptionOrder
	topupQuery := db.Where("tenant_id = ? AND user_id = ?", scope.TenantID, scope.UserID)
	memberQuery := db.Where("user_id = ?", scope.UserID)
	legacyQuery := db.Where("user_id = ?", scope.UserID)
	if refs != nil {
		trades := []string{}
		for _, ref := range refs {
			trades = append(trades, ref.OrderID)
		}
		topupQuery = topupQuery.Where("trade_no IN ?", trades)
		memberQuery = memberQuery.Where("trade_no IN ?", trades)
		legacyQuery = legacyQuery.Where("trade_no IN ?", trades)
	}
	if lock {
		topupQuery = model.InvoiceLockForUpdate(topupQuery)
		memberQuery = model.InvoiceLockForUpdate(memberQuery)
	}
	if len(locked) > 0 && locked[0] != nil {
		topups = locked[0].TopUps
		memberships = locked[0].Memberships
	} else {
		if err := topupQuery.Order("id ASC").Find(&topups).Error; err != nil {
			return nil, err
		}
		if err := memberQuery.Order("id ASC").Find(&memberships).Error; err != nil {
			return nil, err
		}
	}
	if err := legacyQuery.Find(&legacy).Error; err != nil {
		return nil, err
	}
	aliases := map[string]bool{}
	for _, o := range legacy {
		aliases[o.TradeNo] = true
	}
	for _, o := range memberships {
		aliases[o.TradeNo] = true
	}
	invoiceTypes := []string{}
	for _, it := range p.InvoiceTypes {
		invoiceTypes = append(invoiceTypes, it.ID)
	}
	group := invoiceHash(p.SellerID + ":" + p.TaxGroup + ":CNY")
	result := make([]InvoiceCandidate, 0, len(topups)+len(memberships)+len(legacy))
	makeCandidate := func(source, orderNo, provider string, id, amount, paidAt int64, currency string) InvoiceCandidate {
		c := InvoiceCandidate{InvoiceOrderRef: InvoiceOrderRef{SourceType: source, OrderID: orderNo}, OrderNo: orderNo, PaidAt: invoiceTime(paidAt), PaymentLabel: provider, Currency: currency, CurrencyExponent: 2, ReasonCodes: []string{}, MergeGroupID: group, AllowedInvoiceTypes: invoiceTypes, AllowedActions: []string{}, sourceID: id, paidAt: paidAt, amount: amount}
		c.canonicalKey = invoiceHash("payment:v1:0:" + provider + ":" + orderNo)
		if amount > 0 && currency != "" {
			v := strconv.FormatInt(amount, 10)
			c.AmountMinor = &v
		}
		return c
	}
	topupReceipts := map[int64]model.InvoicePaymentReceipt{}
	if len(topups) > 0 {
		var proofRows []model.InvoicePaymentReceipt
		if len(locked) > 0 && locked[0] != nil {
			proofRows = locked[0].TopUpReceipts
		} else {
			ids := make([]int64, 0, len(topups))
			for _, order := range topups {
				ids = append(ids, int64(order.Id))
			}
			if err := db.Where("top_up_id IN ?", ids).Find(&proofRows).Error; err != nil {
				return nil, err
			}
		}
		for _, proof := range proofRows {
			topupReceipts[proof.TopUpID] = proof
		}
	}
	for _, o := range topups {
		if aliases[o.TradeNo] {
			continue
		}
		c := makeCandidate("topup", o.TradeNo, o.PaymentProvider, int64(o.Id), o.PaidMinor, o.CompleteTime, o.Currency)
		if o.Status != common.TopUpStatusSuccess {
			c.ReasonCodes = append(c.ReasonCodes, "UNPAID")
		}
		proof, verified := topupReceipts[int64(o.Id)]
		if len(proof.ReceiptKey) == 64 {
			c.canonicalKey = invoiceHash("payment:receipt:v1:0:" + proof.ReceiptKey)
		}
		verified = verified && proof.TenantID == scope.TenantID && proof.UserID == scope.UserID && proof.Provider == model.PaymentProviderEpay && proof.Provider == o.PaymentProvider && proof.AmountMinor > 0 && proof.AmountMinor == o.PaidMinor && proof.Currency == o.Currency && len(proof.ReceiptKey) == 64 && proof.Status == "verified" && proof.PaidAt > 0
		if !verified || o.CompleteTime <= 0 {
			c.ReasonCodes = append(c.ReasonCodes, "PAYMENT_UNVERIFIED")
			c.AmountMinor = nil
		} else {
			c.amount, c.paidAt, c.PaidAt = proof.AmountMinor, proof.PaidAt, invoiceTime(proof.PaidAt)
		}
		result = append(result, c)
	}
	receiptMap := map[int64][]model.MembershipPaymentReceipt{}
	memberIDs := []int64{}
	for _, o := range memberships {
		memberIDs = append(memberIDs, o.ID)
	}
	if len(memberIDs) > 0 {
		var receiptRows []model.MembershipPaymentReceipt
		if len(locked) > 0 && locked[0] != nil {
			receiptRows = locked[0].Receipts
		} else {
			rq := db.Where("order_id IN ?", memberIDs)
			if lock {
				rq = model.InvoiceLockForUpdate(rq)
			}
			if err := rq.Order("order_id ASC, id ASC").Find(&receiptRows).Error; err != nil {
				return nil, err
			}
		}
		for _, r := range receiptRows {
			receiptMap[r.OrderID] = append(receiptMap[r.OrderID], r)
		}
	}
	for _, o := range memberships {
		c := makeCandidate("membership", o.TradeNo, o.ActualProvider, o.ID, o.ActualAmountMinor, o.PaidAt, o.ActualCurrency)
		if o.ReceiptKey != nil && *o.ReceiptKey != "" {
			c.canonicalKey = invoiceHash("payment:receipt:v1:0:" + invoiceHash(*o.ReceiptKey))
		}
		if o.Provider == model.PaymentProviderBalance {
			c.ReasonCodes = append(c.ReasonCodes, "WALLET_PURCHASE")
			c.AmountMinor = nil
		}
		if o.Status != model.MembershipOrderSuccess {
			c.ReasonCodes = append(c.ReasonCodes, "PAYMENT_EXCEPTION")
		}
		receipts := receiptMap[o.ID]
		valid := o.ReceiptKey != nil && *o.ReceiptKey == o.ActualProvider+":"+o.ReceiptID && o.ActualProvider == model.PaymentProviderEpay && o.PaidAt > 0 && o.ActualOneTime && o.ActualAmountMinor > 0 && o.ActualAmountMinor == o.AmountMinor && o.Currency == o.ActualCurrency && o.Provider == o.ActualProvider && o.ExceptionReason == ""
		primaryCount := 0
		for _, r := range receipts {
			if o.ReceiptKey != nil && r.ReceiptKey == *o.ReceiptKey {
				primaryCount++
				valid = valid && r.Provider == o.Provider && r.ReceiptID == o.ReceiptID && r.AmountMinor == o.ActualAmountMinor && r.Currency == o.ActualCurrency && r.OneTime && r.Status == model.MembershipOrderSuccess
			} else if r.Status != "resolved" || strings.TrimSpace(r.Resolution) == "" {
				valid = false
			}
		}
		valid = valid && primaryCount == 1
		if !valid {
			c.ReasonCodes = append(c.ReasonCodes, "PAYMENT_UNVERIFIED")
			c.AmountMinor = nil
		}
		result = append(result, c)
	}
	for _, o := range legacy {
		c := makeCandidate("legacy_subscription", o.TradeNo, o.PaymentProvider, int64(o.Id), 0, o.CompleteTime, "")
		c.ReasonCodes = append(c.ReasonCodes, "PAYMENT_UNVERIFIED")
		result = append(result, c)
	}
	var claims []model.InvoiceOrderClaim
	keys := make([]string, 0, len(result))
	for _, c := range result {
		keys = append(keys, c.canonicalKey)
	}
	claimQuery := db.Where("tenant_id = ? AND canonical_payment_key IN ?", scope.TenantID, keys)
	if len(locked) > 0 && locked[0] != nil {
		claimQuery = model.InvoiceLockForUpdate(claimQuery).Order("canonical_payment_key ASC")
	}
	if err := claimQuery.Find(&claims).Error; err != nil {
		return nil, err
	}
	claimed := map[string]int64{}
	requestIDs := []int64{}
	for _, c := range claims {
		claimed[c.CanonicalPaymentKey] = c.RequestID
		requestIDs = append(requestIDs, c.RequestID)
	}
	requests := map[int64]string{}
	if len(requestIDs) > 0 {
		var rows []model.InvoiceRequest
		if err := db.Select("id", "public_id").Where("id IN ? AND tenant_id = ? AND user_id = ?", requestIDs, scope.TenantID, scope.UserID).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			requests[r.ID] = r.PublicID
		}
	}
	for i := range result {
		c := &result[i]
		if !factsOnly && !p.Enabled {
			c.ReasonCodes = append(c.ReasonCodes, p.DisabledReason)
		}
		if !factsOnly && (!slices.Contains(p.Sources, c.SourceType) || !slices.Contains(p.Providers, c.PaymentLabel)) {
			c.ReasonCodes = append(c.ReasonCodes, "SOURCE_DISABLED")
		}
		if c.Currency != "CNY" {
			c.ReasonCodes = append(c.ReasonCodes, "CURRENCY_UNSUPPORTED")
		}
		if id, ok := claimed[c.canonicalKey]; ok {
			c.ReasonCodes = append(c.ReasonCodes, "ORDER_CLAIMED")
			c.ClaimedRequestPublicID = requests[id]
		}
		c.Eligible = len(c.ReasonCodes) == 0
		if c.Eligible {
			c.AllowedActions = []string{"select"}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].paidAt == result[j].paidAt {
			return result[i].OrderID > result[j].OrderID
		}
		return result[i].paidAt > result[j].paidAt
	})
	return result, nil
}

func (s InvoiceService) Candidates(ctx context.Context, scope InvoiceScope, f InvoiceFilter) (InvoicePage[InvoiceCandidate], error) {
	page := InvoicePage[InvoiceCandidate]{Items: []InvoiceCandidate{}}
	if err := invoicePaging(&f); err != nil {
		return page, err
	}
	page.P, page.PageSize = f.P, f.PageSize
	if f.Eligibility != "" && f.Eligibility != "eligible" && f.Eligibility != "unavailable" {
		return page, invoiceError(400, "VALIDATION_FAILED")
	}
	rows, err := s.candidateRows(ctx, scope, s.database(ctx), false)
	if err != nil {
		return page, err
	}
	offset := (f.P - 1) * f.PageSize
	for _, c := range rows {
		if f.SourceType != "" && c.SourceType != f.SourceType || f.OrderID != "" && c.OrderID != f.OrderID || f.Currency != "" && c.Currency != f.Currency || f.Q != "" && !strings.Contains(strings.ToLower(c.OrderNo), strings.ToLower(f.Q)) || !invoiceDateMatches(c.paidAt, f) {
			continue
		}
		eligibility := f.Eligibility
		if eligibility == "" {
			eligibility = "eligible"
		}
		if eligibility == "eligible" && !c.Eligible || eligibility == "unavailable" && c.Eligible {
			continue
		}
		if page.Total >= int64(offset) && len(page.Items) < f.PageSize {
			page.Items = append(page.Items, c)
		}
		page.Total++
	}
	return page, nil
}
func (s InvoiceService) Quote(ctx context.Context, scope InvoiceScope, input InvoiceQuoteInput) (InvoiceQuote, error) {
	return s.quote(ctx, scope, s.database(ctx), input, false, 0)
}
func (s InvoiceService) quote(ctx context.Context, scope InvoiceScope, db *gorm.DB, input InvoiceQuoteInput, lock bool, claimRequestID int64, locked ...*invoicePaymentSnapshot) (InvoiceQuote, error) {
	quote := InvoiceQuote{Items: []InvoiceCandidate{}, AmountMinor: "0", PolicyVersion: s.Policy().PolicyVersion, Eligible: true}
	if len(input.Items) < 1 || len(input.Items) > 50 {
		return quote, invoiceError(400, "VALIDATION_FAILED")
	}
	if input.PolicyVersion != "" && input.PolicyVersion != quote.PolicyVersion {
		return quote, invoiceError(409, "POLICY_CHANGED")
	}
	if lock {
		snapshot, err := s.lockQuotePayments(db, scope, input.Items)
		if err != nil {
			return quote, err
		}
		locked = []*invoicePaymentSnapshot{snapshot}
	}
	rows, err := s.candidateRowsFor(ctx, scope, db, false, input.Items, claimRequestID > 0, locked...)
	if err != nil {
		return quote, err
	}
	byRef := map[string]InvoiceCandidate{}
	for _, c := range rows {
		byRef[c.SourceType+":"+c.OrderID] = c
	}
	selected := map[string]bool{}
	var total int64
	for _, ref := range input.Items {
		c, ok := byRef[ref.SourceType+":"+ref.OrderID]
		if !ok {
			return quote, gorm.ErrRecordNotFound
		}
		if selected[c.canonicalKey] {
			return quote, invoiceError(400, "DUPLICATE_PAYMENT")
		}
		selected[c.canonicalKey] = true
		if claimRequestID > 0 {
			var claim model.InvoiceOrderClaim
			err := db.Where("tenant_id = ? AND canonical_payment_key = ?", scope.TenantID, c.canonicalKey).First(&claim).Error
			if err == nil && claim.RequestID == claimRequestID {
				c.ReasonCodes = slices.DeleteFunc(c.ReasonCodes, func(r string) bool { return r == "ORDER_CLAIMED" })
				c.Eligible = len(c.ReasonCodes) == 0
			} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return quote, err
			}
		}
		quote.Items = append(quote.Items, c)
		if !c.Eligible {
			quote.Eligible = false
			continue
		}
		if quote.MergeGroupID == "" {
			quote.MergeGroupID = c.MergeGroupID
			quote.Currency = c.Currency
		}
		if quote.MergeGroupID != c.MergeGroupID || quote.Currency != c.Currency {
			return quote, invoiceError(422, "INCOMPATIBLE_GROUP")
		}
		if c.amount <= 0 || total > math.MaxInt64-c.amount {
			return quote, invoiceError(422, "INVOICE_METADATA_MISMATCH")
		}
		total += c.amount
	}
	quote.AmountMinor = strconv.FormatInt(total, 10)
	return quote, nil
}

// lockQuotePayments acquires only selected payment rows in their canonical order.
func (s InvoiceService) lockQuotePayments(tx *gorm.DB, scope InvoiceScope, refs []InvoiceOrderRef) (*invoicePaymentSnapshot, error) {
	snapshot := &invoicePaymentSnapshot{}
	type paymentLock struct {
		key, provider string
		ref           InvoiceOrderRef
	}
	locks := []paymentLock{}
	trades := []string{}
	for _, ref := range refs {
		if ref.SourceType != "topup" && ref.SourceType != "membership" {
			return nil, invoiceError(409, "PAYMENT_CHANGED")
		}
		trades = append(trades, ref.OrderID)
	}
	// Discovery is only for immutable identities and lock order. Never use these
	// consistent-read payment facts for eligibility under MySQL repeatable-read.
	var topups []model.TopUp
	if err := tx.Where("tenant_id = ? AND user_id = ? AND trade_no IN ?", scope.TenantID, scope.UserID, trades).Find(&topups).Error; err != nil {
		return nil, err
	}
	var members []model.MembershipOrder
	if err := tx.Where("user_id = ? AND trade_no IN ?", scope.UserID, trades).Find(&members).Error; err != nil {
		return nil, err
	}
	found := map[string]bool{}
	for _, row := range topups {
		ref := InvoiceOrderRef{SourceType: "topup", OrderID: row.TradeNo}
		found[ref.SourceType+":"+ref.OrderID] = true
		locks = append(locks, paymentLock{key: invoiceHash("payment:v1:0:" + row.PaymentProvider + ":" + row.TradeNo), provider: row.PaymentProvider, ref: ref})
	}
	for _, row := range members {
		ref := InvoiceOrderRef{SourceType: "membership", OrderID: row.TradeNo}
		found[ref.SourceType+":"+ref.OrderID] = true
		locks = append(locks, paymentLock{key: invoiceHash("payment:v1:0:" + row.ActualProvider + ":" + row.TradeNo), provider: row.ActualProvider, ref: ref})
	}
	for _, ref := range refs {
		if !found[ref.SourceType+":"+ref.OrderID] {
			return nil, gorm.ErrRecordNotFound
		}
	}
	sort.Slice(locks, func(i, j int) bool {
		if locks[i].key == locks[j].key {
			return locks[i].ref.SourceType < locks[j].ref.SourceType
		}
		return locks[i].key < locks[j].key
	})
	for _, lock := range locks {
		switch lock.ref.SourceType {
		case "topup":
			var row model.TopUp
			if err := model.InvoiceLockForUpdate(tx).Where("tenant_id = ? AND user_id = ? AND trade_no = ?", scope.TenantID, scope.UserID, lock.ref.OrderID).First(&row).Error; err != nil {
				return nil, err
			}
			if row.PaymentProvider != lock.provider {
				return nil, invoiceError(409, "PAYMENT_CHANGED")
			}
			snapshot.TopUps = append(snapshot.TopUps, row)
			var proofs []model.InvoicePaymentReceipt
			if err := model.InvoiceLockForUpdate(tx).Where("top_up_id = ?", row.Id).Find(&proofs).Error; err != nil {
				return nil, err
			}
			snapshot.TopUpReceipts = append(snapshot.TopUpReceipts, proofs...)
		case "membership":
			var row model.MembershipOrder
			if err := model.InvoiceLockForUpdate(tx).Where("user_id = ? AND trade_no = ?", scope.UserID, lock.ref.OrderID).First(&row).Error; err != nil {
				return nil, err
			}
			if row.ActualProvider != lock.provider {
				return nil, invoiceError(409, "PAYMENT_CHANGED")
			}
			snapshot.Memberships = append(snapshot.Memberships, row)
		}
	}
	// Receipt current reads follow all canonical payment anchors; payment writers also hold
	// their anchor before touching receipts, so these reads cannot race a payment anomaly.
	sort.Slice(snapshot.Memberships, func(i, j int) bool { return snapshot.Memberships[i].ID < snapshot.Memberships[j].ID })
	for _, order := range snapshot.Memberships {
		var receipts []model.MembershipPaymentReceipt
		if err := model.InvoiceLockForUpdate(tx).Where("order_id = ?", order.ID).Order("id ASC").Find(&receipts).Error; err != nil {
			return nil, err
		}
		snapshot.Receipts = append(snapshot.Receipts, receipts...)
	}
	// Claims precede request locks, including during revalidation of an existing
	// request. Payment callbacks use the same order when they hold an invoice.
	keys := make([]string, 0, len(snapshot.TopUpReceipts)+len(snapshot.Memberships))
	for _, proof := range snapshot.TopUpReceipts {
		keys = append(keys, invoiceHash("payment:receipt:v1:0:"+proof.ReceiptKey))
	}
	for _, order := range snapshot.Memberships {
		if order.ReceiptKey != nil {
			keys = append(keys, invoiceHash("payment:receipt:v1:0:"+invoiceHash(*order.ReceiptKey)))
		}
	}
	var claims []model.InvoiceOrderClaim
	if err := model.InvoiceLockForUpdate(tx).Where("tenant_id = ? AND canonical_payment_key IN ?", scope.TenantID, keys).Order("canonical_payment_key ASC").Find(&claims).Error; err != nil {
		return nil, err
	}
	return snapshot, nil
}
