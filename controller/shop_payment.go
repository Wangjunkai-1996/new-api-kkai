package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func shopReceiptDTO(row model.ShopPaymentReceipt, orderNo any) gin.H {
	return gin.H{"id": row.PublicID, "version": shopID(row.Version), "order_no": orderNo, "provider": row.Provider, "provider_transaction_id": row.ProviderTransactionID, "money": shopMoney(row.AmountMinor, row.Currency, row.CurrencyExponent), "state": row.ProcessingState, "public_summary": row.ExceptionCode, "allowed_actions": []string{}, "evidence_ids": []string{}, "refunds": []gin.H{}, "adjustments": []gin.H{}, "refundable_money_by_kind": gin.H{}}
}

func shopRefundDTO(row model.ShopRefundCase, receiptID string, orderNo any) gin.H {
	return gin.H{"id": row.PublicID, "version": shopID(row.Version), "receipt_id": receiptID, "order_no": orderNo, "kind": row.Kind, "state": row.State, "money": shopMoney(row.AmountMinor, row.Currency, row.CurrencyExponent), "allowed_actions": []string{}, "attempts": []gin.H{}, "evidence_ids": []string{}, "public_note": ""}
}

// Bulk reference projection keeps list reads bounded independently of page size.
func shopOrderReferences(c *gin.Context, ids []int64) (map[int64]string, bool) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, true
	}
	var rows []model.ShopOrder
	if err := shopDB(c).Select("id", "public_id").Where("id IN ? AND tenant_id = ?", ids, model.PlatformTenantID).Find(&rows).Error; err != nil {
		shopModelError(c, err)
		return nil, false
	}
	for _, row := range rows {
		out[row.ID] = row.PublicID
	}
	return out, true
}

func ShopListReceipts(c *gin.Context) {
	page, size, ok := shopPagination(c)
	if !ok {
		return
	}
	rows, total, err := model.ListShopPaymentReceipts(shopDB(c), shopScope(c), model.ShopRecordFilter{Page: page, Limit: size, OrderPublicID: c.Query("order_no"), State: c.Query("state")})
	if err != nil {
		shopModelError(c, err)
		return
	}
	ids := []int64{}
	for _, row := range rows {
		if row.OrderID != nil {
			ids = append(ids, *row.OrderID)
		}
	}
	orders, ok := shopOrderReferences(c, ids)
	if !ok {
		return
	}
	items := []gin.H{}
	for _, row := range rows {
		var orderNo any
		if row.OrderID != nil {
			orderNo = orders[*row.OrderID]
		}
		items = append(items, shopReceiptDTO(row, orderNo))
	}
	shopSuccess(c, 200, shopPage(items, page, size, total))
}

func ShopGetReceipt(c *gin.Context) { shopRespondReceipt(c, c.Param("receipt_id")) }
func shopRespondReceipt(c *gin.Context, id string) {
	row, err := model.GetShopPaymentReceipt(shopDB(c), shopScope(c), id)
	if err != nil {
		shopModelError(c, err)
		return
	}
	var orderNo any
	if row.OrderID != nil {
		orders, ok := shopOrderReferences(c, []int64{*row.OrderID})
		if !ok {
			return
		}
		orderNo = orders[*row.OrderID]
	}
	out := shopReceiptDTO(*row, orderNo)
	amounts, err := model.GetShopRefundableAmounts(shopDB(c), shopScope(c), id)
	if err != nil {
		shopModelError(c, err)
		return
	}
	moneyByKind := gin.H{}
	actions := []string{"assign", "reconcile", "create_evidence", "record_adjustment", "review_adjustment"}
	for kind, amount := range amounts {
		if amount > 0 {
			moneyByKind[kind] = shopMoney(amount, row.Currency, row.CurrencyExponent)
		}
	}
	if len(moneyByKind) > 0 {
		actions = append(actions, "refund")
	}
	out["refundable_money_by_kind"], out["allowed_actions"] = moneyByKind, actions
	evidence, _, err := model.ListShopPaymentEvidence(shopDB(c), shopScope(c), id, 1, 100)
	if err != nil {
		shopModelError(c, err)
		return
	}
	evidenceIDs := []string{}
	for _, item := range evidence {
		evidenceIDs = append(evidenceIDs, item.PublicID)
	}
	out["evidence_ids"] = evidenceIDs
	refunds, _, err := model.ListShopRefunds(shopDB(c), shopScope(c), model.ShopRecordFilter{ReceiptPublicID: id, Page: 1, Limit: 100})
	if err != nil {
		shopModelError(c, err)
		return
	}
	refundItems := []gin.H{}
	for _, item := range refunds {
		refundItems = append(refundItems, shopRefundDTO(item, id, orderNo))
	}
	out["refunds"] = refundItems
	adjustments, _, err := model.ListShopPaymentAdjustments(shopDB(c), shopScope(c), id, 1, 100)
	if err != nil {
		shopModelError(c, err)
		return
	}
	adjustmentItems := []gin.H{}
	for _, item := range adjustments {
		kind := item.Kind
		if kind == "refund" {
			kind = "external_refund"
		}
		adjustmentItems = append(adjustmentItems, gin.H{"id": item.PublicID, "kind": kind, "state": item.State, "money": shopMoney(item.AmountMinor, item.Currency, item.CurrencyExponent)})
	}
	out["adjustments"] = adjustmentItems
	shopSuccess(c, 200, out)
}

func ShopListRefunds(c *gin.Context) {
	page, size, ok := shopPagination(c)
	if !ok {
		return
	}
	rows, total, err := model.ListShopRefunds(shopDB(c), shopScope(c), model.ShopRecordFilter{Page: page, Limit: size, OrderPublicID: c.Query("order_no"), ReceiptPublicID: c.Query("receipt_id"), State: c.Query("state"), Kind: c.Query("kind")})
	if err != nil {
		shopModelError(c, err)
		return
	}
	orderIDs, receiptIDs := []int64{}, []int64{}
	for _, row := range rows {
		receiptIDs = append(receiptIDs, row.ReceiptID)
		if row.OrderID != nil {
			orderIDs = append(orderIDs, *row.OrderID)
		}
	}
	orders, ok := shopOrderReferences(c, orderIDs)
	if !ok {
		return
	}
	var receipts []model.ShopPaymentReceipt
	if len(receiptIDs) > 0 {
		if err = shopDB(c).Select("id", "public_id").Where("id IN ?", receiptIDs).Find(&receipts).Error; err != nil {
			shopModelError(c, err)
			return
		}
	}
	byID := map[int64]string{}
	for _, row := range receipts {
		byID[row.ID] = row.PublicID
	}
	items := []gin.H{}
	for _, row := range rows {
		var orderNo any
		if row.OrderID != nil {
			orderNo = orders[*row.OrderID]
		}
		items = append(items, shopRefundDTO(row, byID[row.ReceiptID], orderNo))
	}
	shopSuccess(c, 200, shopPage(items, page, size, total))
}

func ShopGetRefund(c *gin.Context) { shopRespondRefund(c, c.Param("refund_id")) }
func shopRespondRefund(c *gin.Context, id string) {
	row, err := model.GetShopRefund(shopDB(c), shopScope(c), id)
	if err != nil {
		shopModelError(c, err)
		return
	}
	var receipt model.ShopPaymentReceipt
	if err = shopDB(c).Select("public_id").First(&receipt, row.ReceiptID).Error; err != nil {
		shopModelError(c, err)
		return
	}
	var orderNo any
	if row.OrderID != nil {
		refs, ok := shopOrderReferences(c, []int64{*row.OrderID})
		if !ok {
			return
		}
		orderNo = refs[*row.OrderID]
	}
	out := shopRefundDTO(*row, receipt.PublicID, orderNo)
	attempts, _, err := model.ListShopRefundAttempts(shopDB(c), shopScope(c), id, 1, 100)
	if err != nil {
		shopModelError(c, err)
		return
	}
	items, actions, evidenceIDs := []gin.H{}, []string{}, []string{}
	active := row.State != "succeeded" && row.State != "cancelled"
	var adjusted int64
	if err = shopDB(c).Model(&model.ShopPaymentAdjustment{}).Where("receipt_id = ? AND state <> ?", row.ReceiptID, "failed").Count(&adjusted).Error; err != nil {
		shopModelError(c, err)
		return
	}
	canCancel := active && adjusted == 0
	canRegister := (row.State == "approved" || row.State == "failed") && adjusted == 0
	for _, item := range attempts {
		items = append(items, gin.H{"id": item.PublicID, "state": item.State, "channel_ref": item.ProviderRefundRef, "operated_at": shopTime(item.OperatedAt)})
		var ids []string
		if common.UnmarshalJsonStr(item.EvidenceIDsJSON, &ids) == nil {
			evidenceIDs = append(evidenceIDs, ids...)
		}
		if item.State != "failed" && item.State != "registered" {
			canCancel = false
		}
		if item.State != "failed" {
			canRegister = false
		}
		if item.State == "registered" && (item.OperatedAt != 0 || item.ProviderRefundRef != "") {
			canCancel = false
		}
		if active && item.State == "registered" {
			actions = append(actions, "record_attempt")
		}
		if active && (item.State == "registered" || item.State == "unknown") {
			actions = append(actions, "verify_attempt")
		}
	}
	if canRegister {
		actions = append(actions, "register_attempt")
	}
	if canCancel {
		actions = append(actions, "cancel")
	}
	out["attempts"], out["allowed_actions"], out["evidence_ids"] = items, actions, evidenceIDs
	shopSuccess(c, 200, out)
}

func ShopReceiptAction(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in struct {
			ExpectedVersion string `json:"expected_version"`
			AssigneeID      string `json:"assignee_id"`
			Reason          string `json:"reason"`
		}
		if !shopBind(c, &in, 8192) {
			return
		}
		id := c.Param("receipt_id")
		if _, err := model.GetShopPaymentReceipt(shopDB(c), shopScope(c), id); err != nil {
			shopModelError(c, err)
			return
		}
		cmd, ok := shopCommand(c, in, "receipt-"+action, id, in.ExpectedVersion)
		if !ok {
			return
		}
		assignee := 0
		if action == "assign" {
			value, err := shopVersion(in.AssigneeID)
			if err != nil || value > 2147483647 {
				shopModelError(c, model.ErrShopInvalid)
				return
			}
			assignee = int(value)
		}
		if _, err := model.ReviewShopPaymentReceipt(shopDB(c), model.ShopReceiptReviewInput{Command: cmd, ReceiptPublicID: id, Action: action, AssigneeID: assignee, Reason: in.Reason}); err != nil {
			shopModelError(c, err)
			return
		}
		shopRespondReceipt(c, id)
	}
}

func ShopApproveRefund(c *gin.Context) {
	var in struct {
		ExpectedVersion string   `json:"expected_version"`
		Kind            string   `json:"kind"`
		Reason          string   `json:"reason"`
		EvidenceRefs    []string `json:"evidence_refs"`
	}
	if !shopBind(c, &in, 16384) {
		return
	}
	id := c.Param("receipt_id")
	receipt, err := model.GetShopPaymentReceipt(shopDB(c), shopScope(c), id)
	if err != nil {
		shopModelError(c, err)
		return
	}
	cmd, ok := shopCommand(c, in, "refund-approve", id, in.ExpectedVersion)
	if !ok {
		return
	}
	previous, ok := shopCommitted(c, cmd, "refund-approve", id)
	if !ok {
		return
	}
	if previous != nil {
		shopRespondRefund(c, previous.ResourcePublicID)
		return
	}
	if receipt.Version != cmd.ExpectedVersion {
		shopModelError(c, model.ErrShopVersion)
		return
	}
	amounts, err := model.GetShopRefundableAmounts(shopDB(c), shopScope(c), id)
	if err != nil {
		shopModelError(c, err)
		return
	}
	amount := amounts[in.Kind]
	if amount <= 0 {
		shopModelError(c, model.ErrShopRefund)
		return
	}
	if !shopConsumeProof(c, service.VerificationScopeShopRefundManage, service.ShopVerificationContext{Action: "approve", TargetID: id, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: cmd.Key, Details: service.ShopVerificationDetails{Kind: in.Kind, Money: &service.ShopProofMoney{AmountMinor: shopID(amount), Currency: receipt.Currency, Exponent: receipt.CurrencyExponent}, Reason: in.Reason, EvidenceIDs: in.EvidenceRefs}}) {
		return
	}
	row, err := model.ApproveShopRefund(shopDB(c), model.ShopRefundApproveInput{Command: cmd, ReceiptPublicID: id, Kind: in.Kind, Reason: in.Reason, EvidenceIDs: in.EvidenceRefs})
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopRespondRefund(c, row.PublicID)
}

func ShopRefundAction(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in struct {
			ExpectedVersion       string   `json:"expected_version"`
			Reason                string   `json:"reason"`
			EvidenceRefs          []string `json:"evidence_refs"`
			PreflightEvidenceRefs []string `json:"preflight_evidence_refs"`
			ChannelRef            string   `json:"channel_ref"`
			OperatedAt            string   `json:"operated_at"`
			PublicNote            string   `json:"public_note"`
			Conclusion            string   `json:"conclusion"`
		}
		if !shopBind(c, &in, 16384) {
			return
		}
		// Only record_attempt binds a public note into its operation proof.
		if action != "record" && in.PublicNote != "" {
			shopModelError(c, model.ErrShopInvalid)
			return
		}
		id, attemptID := c.Param("refund_id"), c.Param("attempt_id")
		if _, err := model.GetShopRefund(shopDB(c), shopScope(c), id); err != nil {
			shopModelError(c, err)
			return
		}
		cmd, ok := shopCommand(c, gin.H{"input": in, "attempt_id": attemptID}, "refund-"+action, id, in.ExpectedVersion)
		if !ok {
			return
		}
		previous, ok := shopCommitted(c, cmd, "refund-"+action, id)
		if !ok {
			return
		}
		if previous != nil {
			shopRespondRefund(c, id)
			return
		}
		input := model.ShopRefundActionInput{Command: cmd, RefundPublicID: id, AttemptPublicID: attemptID, Action: action, Reason: in.Reason, PublicNote: in.PublicNote, ProviderRefundRef: in.ChannelRef, Conclusion: in.Conclusion, EvidenceIDs: in.EvidenceRefs}
		proof := service.ShopVerificationContext{TargetID: id, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: cmd.Key}
		switch action {
		case "attempt":
			input.EvidenceIDs = in.PreflightEvidenceRefs
			proof.Action, proof.Details = "register_attempt", service.ShopVerificationDetails{Reason: in.Reason, EvidenceIDs: input.EvidenceIDs}
		case "cancel":
			proof.Action, proof.Details = "cancel", service.ShopVerificationDetails{Reason: in.Reason, EvidenceIDs: in.EvidenceRefs}
		case "record":
			parsed, err := time.Parse(time.RFC3339, in.OperatedAt)
			if err != nil || !strings.HasSuffix(in.OperatedAt, "Z") {
				shopModelError(c, model.ErrShopInvalid)
				return
			}
			input.OperatedAt, input.Reason = parsed.Unix(), "refund_operation_recorded"
			proof.Action, proof.Details = "record_attempt", service.ShopVerificationDetails{AttemptID: attemptID, OperatedAt: in.OperatedAt, EvidenceIDs: in.EvidenceRefs, PublicNote: in.PublicNote, ChannelRef: in.ChannelRef}
		case "verify":
			input.Reason = "refund_result_verified"
			proof.Action, proof.Details = "verify_attempt", service.ShopVerificationDetails{AttemptID: attemptID, Conclusion: in.Conclusion, EvidenceIDs: in.EvidenceRefs}
			// A verification proof must not authorize an unbound reference rewrite.
			if in.ChannelRef != "" {
				shopModelError(c, model.ErrShopInvalid)
				return
			}
		}
		if !shopConsumeProof(c, service.VerificationScopeShopRefundManage, proof) {
			return
		}
		if _, err := model.MutateShopRefund(shopDB(c), input); err != nil {
			shopModelError(c, err)
			return
		}
		shopRespondRefund(c, id)
	}
}

func ShopRecordAdjustment(c *gin.Context) { shopAdjustment(c, false) }
func ShopReviewAdjustment(c *gin.Context) { shopAdjustment(c, true) }
func shopAdjustment(c *gin.Context, review bool) {
	var in struct {
		ExpectedVersion       string                  `json:"expected_version"`
		Kind                  string                  `json:"kind"`
		ProviderAdjustmentRef string                  `json:"provider_adjustment_ref"`
		Money                 *service.ShopProofMoney `json:"money"`
		State                 string                  `json:"state"`
		Reason                string                  `json:"reason"`
		EvidenceRefs          []string                `json:"evidence_refs"`
	}
	if !shopBind(c, &in, 16384) {
		return
	}
	id, adjustmentID, action := c.Param("receipt_id"), "", "payment-adjustment"
	receipt, err := model.GetShopPaymentReceipt(shopDB(c), shopScope(c), id)
	if err != nil {
		shopModelError(c, err)
		return
	}
	var old *model.ShopPaymentAdjustment
	if review {
		adjustmentID, action = c.Param("adjustment_id"), "payment-adjustment-review"
		old, err = model.GetShopPaymentAdjustment(shopDB(c), shopScope(c), adjustmentID)
		if err != nil {
			shopModelError(c, err)
			return
		}
		if old.ReceiptID != receipt.ID {
			shopModelError(c, model.ErrShopNotFound)
			return
		}
		if in.Money != nil || in.Kind != "" || in.ProviderAdjustmentRef != "" {
			shopModelError(c, model.ErrShopInvalid)
			return
		}
	}
	cmd, ok := shopCommand(c, gin.H{"input": in, "adjustment_id": adjustmentID}, action, id, in.ExpectedVersion)
	if !ok {
		return
	}
	previous, ok := shopCommitted(c, cmd, action, id)
	if !ok {
		return
	}
	if previous != nil {
		shopRespondReceipt(c, id)
		return
	}
	input := model.ShopAdjustmentInput{Command: cmd, ReceiptPublicID: id, AdjustmentPublicID: adjustmentID, State: in.State, Reason: in.Reason, EvidenceIDs: in.EvidenceRefs}
	proof := service.ShopVerificationContext{Action: "record", TargetID: id, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: cmd.Key, Details: service.ShopVerificationDetails{Kind: in.Kind, Money: in.Money, ProviderAdjustmentRef: in.ProviderAdjustmentRef, State: in.State, Reason: in.Reason, EvidenceIDs: in.EvidenceRefs}}
	if review {
		input.Kind, input.ProviderAdjustmentRef, input.AmountMinor, input.Currency, input.CurrencyExponent = old.Kind, old.ProviderAdjustmentRef, old.AmountMinor, old.Currency, old.CurrencyExponent
		proof.Action, proof.Details = "review", service.ShopVerificationDetails{AdjustmentID: adjustmentID, State: in.State, Reason: in.Reason, EvidenceIDs: in.EvidenceRefs}
	} else {
		if in.Money == nil {
			shopModelError(c, model.ErrShopInvalid)
			return
		}
		amount, err := shopVersion(in.Money.AmountMinor)
		if err != nil {
			shopModelError(c, err)
			return
		}
		input.Kind, input.ProviderAdjustmentRef, input.AmountMinor, input.Currency, input.CurrencyExponent = in.Kind, in.ProviderAdjustmentRef, amount, in.Money.Currency, in.Money.Exponent
	}
	if !shopConsumeProof(c, service.VerificationScopeShopAdjustmentManage, proof) {
		return
	}
	if _, err = model.RecordShopPaymentAdjustment(shopDB(c), input); err != nil {
		shopModelError(c, err)
		return
	}
	shopRespondReceipt(c, id)
}

func ShopCreateEvidence(c *gin.Context) {
	var in struct {
		ReceiptID             string                 `json:"receipt_id"`
		ReceiptVersion        string                 `json:"receipt_version"`
		Source                string                 `json:"source"`
		ProviderTransactionID string                 `json:"provider_transaction_id"`
		ProviderAdjustmentID  string                 `json:"provider_refund_or_adjustment_id"`
		Money                 service.ShopProofMoney `json:"money"`
		ObservedStatus        string                 `json:"observed_status"`
		ObservedAt            string                 `json:"observed_at"`
		Reference             string                 `json:"reference"`
		Summary               string                 `json:"summary"`
		CorrectionOf          string                 `json:"correction_of"`
	}
	if !shopBind(c, &in, 16384) {
		return
	}
	if _, err := model.GetShopPaymentReceipt(shopDB(c), shopScope(c), in.ReceiptID); err != nil {
		shopModelError(c, err)
		return
	}
	cmd, ok := shopCommand(c, in, "payment-evidence", in.ReceiptID, in.ReceiptVersion)
	if !ok {
		return
	}
	previous, ok := shopCommitted(c, cmd, "payment-evidence", in.ReceiptID)
	if !ok {
		return
	}
	if previous != nil {
		shopSuccess(c, 200, gin.H{"evidence_id": previous.ResourcePublicID})
		return
	}
	amount, err := strconv.ParseInt(in.Money.AmountMinor, 10, 64)
	observed, dateErr := time.Parse(time.RFC3339, in.ObservedAt)
	if err != nil || amount < 0 || shopID(amount) != in.Money.AmountMinor || dateErr != nil || !strings.HasSuffix(in.ObservedAt, "Z") {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	refHash, summaryHash := sha256.Sum256([]byte(in.Reference)), sha256.Sum256([]byte(in.Summary))
	if !shopConsumeProof(c, service.VerificationScopeShopEvidenceCreate, service.ShopVerificationContext{Action: "create", TargetID: in.ReceiptID, ExpectedVersion: in.ReceiptVersion, IdempotencyKey: cmd.Key, Details: service.ShopVerificationDetails{Source: in.Source, ProviderTransactionID: in.ProviderTransactionID, ProviderRefundOrAdjustmentID: in.ProviderAdjustmentID, Money: &in.Money, ObservedStatus: in.ObservedStatus, ObservedAt: in.ObservedAt, ReferenceDigest: hex.EncodeToString(refHash[:]), SummaryDigest: hex.EncodeToString(summaryHash[:]), CorrectionOf: in.CorrectionOf}}) {
		return
	}
	row, err := model.RegisterShopPaymentEvidence(shopDB(c), model.ShopEvidenceInput{Command: cmd, ReceiptPublicID: in.ReceiptID, CorrectionOfPublicID: in.CorrectionOf, Evidence: model.ShopPaymentEvidence{Source: in.Source, ProviderTransactionID: in.ProviderTransactionID, ProviderAdjustmentRef: in.ProviderAdjustmentID, AmountMinor: amount, Currency: in.Money.Currency, CurrencyExponent: in.Money.Exponent, ObservedStatus: in.ObservedStatus, ObservedAt: observed.Unix(), Reference: in.Reference, Summary: in.Summary}})
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 201, gin.H{"evidence_id": row.PublicID})
}

func ShopGetEvidence(c *gin.Context) {
	row, err := model.GetShopPaymentEvidence(shopDB(c), shopScope(c), c.Param("evidence_id"))
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 200, gin.H{"id": row.PublicID, "source": row.Source, "provider_transaction_id": row.ProviderTransactionID, "provider_refund_or_adjustment_id": row.ProviderAdjustmentRef, "money": shopMoney(row.AmountMinor, row.Currency, row.CurrencyExponent), "observed_status": row.ObservedStatus, "observed_at": shopTime(row.ObservedAt), "reference": row.Reference, "summary": row.Summary})
}

// Epay uses its own signed-form protocol, not the application JSON envelope.
func ShopEpayCallback(c *gin.Context) {
	shopHeaders(c)
	// Encoded forms can be larger than the service's decoded 16 KiB limit.
	if len(c.Request.URL.RawQuery) > 64<<10 {
		c.String(400, "fail")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var values url.Values
	var err error
	if c.Request.Method == http.MethodPost {
		err = c.Request.ParseForm()
		values = c.Request.PostForm
	} else {
		values, err = url.ParseQuery(c.Request.URL.RawQuery)
	}
	if err != nil {
		c.String(400, "fail")
		return
	}
	for _, list := range values {
		if len(list) != 1 {
			c.String(400, "fail")
			return
		}
	}
	accepted, _ := service.HandleShopEpayCallback(c.Request.Context(), shopDB(c), values)
	if !accepted {
		c.String(400, "fail")
		return
	}
	c.String(200, "success")
}
