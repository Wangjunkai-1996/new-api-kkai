package controller

import (
	"net/http"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// Explicit projections keep buyer input and payment credentials out of normal responses.
func shopOrderDTO(d *model.ShopOrderDetail, admin bool) gin.H {
	o, s := d.Order, d.Snapshot
	now := time.Now().Unix()
	aftersale, refund, status := "none", "none", "awaiting_payment"
	for _, a := range d.Aftersales {
		if o.ActiveAftersaleID != nil && a.ID == *o.ActiveAftersaleID {
			aftersale = a.State
			break
		}
	}
	var refundSummary any
	extras := []gin.H{}
	blocked := model.ShopOrderDeliveryBlocked(d)
	for _, r := range d.Refunds {
		summary := gin.H{"id": r.PublicID, "state": r.State, "money": shopMoney(r.AmountMinor, r.Currency, r.CurrencyExponent), "public_summary": r.State}
		if r.Kind == "goods" && refundSummary == nil {
			refund = r.State
			refundSummary = summary
			if r.State != "cancelled" {
				blocked = true
			}
		} else {
			extras = append(extras, summary)
		}
	}
	if o.PurchaseState == "closed" {
		status = "closed"
	}
	if o.PaymentState == "verifying" {
		status = "payment_verifying"
	}
	if o.PaymentState == "paid" {
		switch o.DeliveryState {
		case "pending":
			status = "pending_delivery"
		case "processing":
			status = "processing"
		case "awaiting_input":
			status = "awaiting_input"
		case "exception":
			status = "delivery_exception"
		case "delivered":
			status = "delivered"
		}
	}
	if aftersale == "pending" || aftersale == "processing" {
		status = "aftersale"
	}
	switch refund {
	case "approved", "processing", "unknown":
		status = "refund_pending"
	case "failed":
		status = "refund_failed"
	case "succeeded":
		status = "refunded"
	}
	actions, notices := []string{}, []string{}
	if o.PaymentState != "paid" {
		if o.PurchaseState == "open" {
			actions = append(actions, "close")
		}
		canPay := o.PaymentState == "unpaid"
		if o.ActiveAttemptID != nil {
			canPay = false
			for _, attempt := range d.Attempts {
				if attempt.ID == *o.ActiveAttemptID {
					state := shopPaymentState(attempt)
					canPay = state == "ready" || state == "closed" || state == "failed"
					break
				}
			}
		}
		if o.PurchaseState == "open" && canPay && !o.RiskHold && now < o.ReservationExpiresAt {
			actions = append(actions, "pay")
		}
		if o.ActiveAttemptID != nil {
			actions = append(actions, "reconcile")
		}
	}
	if o.DeliveryState != "delivered" && (o.PurchaseState == "open" || o.PaymentState == "paid") && len(s.Terms.BuyerInputSchema) > 0 {
		actions = append(actions, "provide_input")
	}
	if o.PaymentState == "paid" && aftersale != "pending" && aftersale != "processing" && refund != "succeeded" {
		actions = append(actions, "create_aftersale")
	}
	if o.RiskHold {
		notices = append(notices, "SHOP_RISK_HOLD")
	}
	if o.ExceptionCode != "" {
		notices = append(notices, o.ExceptionCode)
	}
	history := []gin.H{}
	var current, deliveredAt any
	deliveries := d.Deliveries
	if d.CurrentDelivery != nil && !slices.ContainsFunc(deliveries, func(v model.ShopDelivery) bool { return v.ID == d.CurrentDelivery.ID }) {
		deliveries = append(slices.Clone(deliveries), *d.CurrentDelivery)
	}
	for _, v := range deliveries {
		can := !blocked && o.CurrentDeliveryID != nil && *o.CurrentDeliveryID == v.ID && v.RevealRevokedAt == 0 && v.SecretPurgedAt == 0 && now < v.ViewExpiresAt
		fields := []gin.H{}
		for _, f := range s.Terms.DeliveryFields {
			fields = append(fields, gin.H{"key": f.Key, "label": f.Label, "masked_value": "••••••"})
		}
		item := gin.H{"id": v.PublicID, "version": shopID(v.Version), "delivered_at": shopTime(v.DeliveredAt), "view_expires_at": shopTime(v.ViewExpiresAt), "masked_fields": fields, "instructions": v.Instructions, "can_reveal": can, "unavailable_reason_code": nil}
		if !can {
			item["unavailable_reason_code"] = "SHOP_REVEAL_BLOCKED"
			if now >= v.ViewExpiresAt {
				item["unavailable_reason_code"] = "SHOP_REVEAL_EXPIRED"
			}
		}
		if o.CurrentDeliveryID != nil && *o.CurrentDeliveryID == v.ID {
			current = item
			deliveredAt = shopTime(v.DeliveredAt)
			if can {
				actions = append(actions, "reveal")
			}
		} else {
			history = append(history, item)
		}
	}
	timeline := []gin.H{}
	for _, v := range d.Events {
		if admin || v.Visibility == "public" {
			timeline = append(timeline, shopEventDTO(v))
		}
	}
	missing := []string{}
	if o.MissingInputKeysJSON != "" {
		_ = common.UnmarshalJsonStr(o.MissingInputKeysJSON, &missing)
	}
	if missing == nil {
		missing = []string{}
	}
	var risk any
	if o.RiskHold {
		risk = gin.H{"public_reason": o.RiskPublicNote, "started_at": shopTime(o.RiskChangedAt)}
	}
	snapshot := gin.H{"id": s.ProductPublicID, "version": shopID(o.ProductVersion), "name": s.Name, "specification": s.Specification, "cover": nil, "product_type": s.ProductKind, "delivery_type": s.DeliveryType, "price": shopMoney(o.AmountMinor, o.Currency, o.CurrencyExponent), "terms_version": shopID(o.TermsVersion), "template_version": shopID(s.Terms.TemplateVersion), "delivery_fields": s.Terms.DeliveryFields, "buyer_input_schema": s.Terms.BuyerInputSchema, "usage_conditions": s.Terms.UsageConditions, "delivery_policy": s.Terms.DeliveryPolicy, "view_policy": s.Terms.ViewPolicy, "warranty_policy": s.Terms.WarrantyPolicy, "entitlement_policy": s.Terms.EntitlementPolicy, "refund_policy": s.Terms.RefundPolicy}
	if s.CoverRef != "" {
		snapshot["cover"] = gin.H{"url": s.CoverRef, "alt": s.Name}
	}
	if s.Terms.DeliveryFields == nil {
		snapshot["delivery_fields"] = []model.ShopField{}
	}
	if s.Terms.BuyerInputSchema == nil {
		snapshot["buyer_input_schema"] = []model.ShopField{}
	}
	var received, lastChecked any
	for _, r := range d.Receipts {
		if o.EffectiveReceiptID != nil && *o.EffectiveReceiptID == r.ID {
			received = shopMoney(r.AmountMinor, r.Currency, r.CurrencyExponent)
		}
	}
	for _, attempt := range d.Attempts {
		if o.ActiveAttemptID != nil && attempt.ID == *o.ActiveAttemptID {
			lastChecked = shopTime(attempt.LastCheckedAt)
			break
		}
	}
	out := gin.H{"order_no": o.PublicID, "version": shopID(o.Version), "product_snapshot": snapshot, "quantity": o.Quantity, "total": shopMoney(o.AmountMinor, o.Currency, o.CurrencyExponent), "created_at": shopTime(o.CreatedAt), "purchase_state": o.PurchaseState, "payment_state": o.PaymentState, "delivery_state": o.DeliveryState, "aftersale_state": aftersale, "refund_state": refund, "display_status": status, "notice_codes": notices, "allowed_actions": actions, "reservation_expires_at": shopTime(o.ReservationExpiresAt), "paid_at": shopTime(o.PaidAt), "delivery_due_at": shopTime(o.DeliveryDueAt), "delivered_at": deliveredAt, "server_time": shopTime(now), "current_delivery": current, "delivery_history": history, "buyer_input_status": gin.H{"missing_field_keys": missing, "complete": o.InputCompleteAt > 0 && len(missing) == 0}, "risk_hold": risk, "payment_summary": gin.H{"expected": shopMoney(o.AmountMinor, o.Currency, o.CurrencyExponent), "received": received, "public_summary": o.PaymentState, "last_checked_at": lastChecked}, "refund_summary": refundSummary, "extra_payment_summary": extras, "timeline": timeline}
	if received == nil {
		delete(out["payment_summary"].(gin.H), "received")
	}
	if o.ActiveAftersaleID != nil {
		actions = append(actions, "view_aftersale")
		out["allowed_actions"] = actions
	}
	if admin {
		adminActions := []string{}
		names := map[string]string{"claim-order": "claim", "order-note": "note", "risk-hold": "risk_hold", "risk-release": "risk_release", "request-input": "request_input", "retry-delivery": "retry_delivery"}
		for _, action := range model.ShopAdminOrderActions(d) {
			adminActions = append(adminActions, names[action])
		}
		if len(s.Terms.BuyerInputSchema) > 0 && o.BuyerInputPurgedAt == 0 {
			adminActions = append(adminActions, "view_buyer_input")
		}
		if !blocked && o.CurrentDeliveryID == nil && s.DeliveryType == "manual" && o.InputCompleteAt > 0 {
			adminActions = append(adminActions, "deliver")
		}
		if slices.Contains(actions, "reveal") {
			adminActions = append(adminActions, "reveal_delivery")
		}
		out["allowed_actions"] = adminActions
		out["user_id"] = shopID(int64(o.UserID))
		out["assignee_id"] = nil
		if o.ProcessingOwnerID > 0 {
			out["assignee_id"] = shopID(int64(o.ProcessingOwnerID))
		}
	}
	return out
}
func shopEventDTO(e model.ShopEvent) gin.H {
	return gin.H{"event_id": e.PublicID, "event_type": e.Kind, "occurred_at": shopTime(e.CreatedAt), "public_summary": e.Note}
}
func shopRespondOrder(c *gin.Context, id string, status int) {
	d, e := model.GetShopOrderDetail(shopDB(c), shopScope(c), id)
	if e != nil {
		// Mutation callers reach this projection only after committing or finding
		// an idempotent replay. A later authorization/read failure must preserve
		// the original request key without revealing the inaccessible order.
		if c.Request.Method != http.MethodGet {
			shopError(c, http.StatusConflict, "SHOP_REQUEST_UNRESOLVED")
			return
		}
		shopModelError(c, e)
		return
	}
	shopSuccess(c, status, shopOrderDTO(d, shopScope(c).Admin))
}

func ShopCreateOrder(c *gin.Context) {
	var in struct {
		ProductID      string            `json:"product_id"`
		ProductVersion string            `json:"product_version"`
		TermsVersion   string            `json:"terms_version"`
		AcceptedTerms  bool              `json:"accepted_terms"`
		BuyerInput     map[string]string `json:"buyer_input"`
	}
	if !shopBind(c, &in, 8192) {
		return
	}
	cmd, ok := shopCommand(c, in, "create-order", "", "")
	if !ok {
		return
	}
	previous, e := model.GetShopIdempotency(shopDB(c), shopScope(c), "create-order", "", cmd.Key)
	if e != nil {
		shopModelError(c, e)
		return
	}
	if previous != nil {
		shopRespondOrder(c, previous.ResourcePublicID, 200)
		return
	}
	pv, e := shopVersion(in.ProductVersion)
	tv, f := shopVersion(in.TermsVersion)
	if e != nil || f != nil || !in.AcceptedTerms {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	p, e := model.GetShopProduct(shopDB(c), shopScope(c), in.ProductID)
	if e != nil {
		shopModelError(c, e)
		return
	}
	var terms model.ShopProductTerms
	if common.UnmarshalJsonStr(p.TermsJSON, &terms) != nil {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	id, e := model.NewShopPublicID("so_")
	if e != nil {
		shopModelError(c, e)
		return
	}
	secret, keys, e := service.EncryptShopBuyerInput(id, terms, in.BuyerInput)
	if e != nil {
		shopModelError(c, e)
		return
	}
	o, e := model.CreateShopOrder(shopDB(c), model.ShopCreateOrderInput{Command: cmd, PublicID: id, ProductPublicID: in.ProductID, ProductVersion: pv, TermsVersion: tv, AcceptedTerms: in.AcceptedTerms, BuyerInput: secret, InputKeys: keys})
	if e != nil {
		shopModelError(c, e)
		return
	}
	shopRespondOrder(c, o.PublicID, http.StatusCreated)
}
func ShopOrderRecovery(c *gin.Context) {
	key := c.Param("key")
	if !shopKeyPattern.MatchString(key) {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	p, e := model.GetShopIdempotency(shopDB(c), shopScope(c), "create-order", "", key)
	if e != nil {
		shopModelError(c, e)
		return
	}
	out := gin.H{"status": "not_observed", "order_no": nil}
	if p != nil {
		if _, e = model.GetShopOrder(shopDB(c), shopScope(c), p.ResourcePublicID); e != nil {
			shopModelError(c, e)
			return
		}
		out["status"] = "recorded"
		out["order_no"] = p.ResourcePublicID
	}
	shopSuccess(c, 200, out)
}
func ShopGetOrder(c *gin.Context) { shopRespondOrder(c, c.Param("order_no"), 200) }
func ShopListOrders(c *gin.Context) {
	page, size, ok := shopPagination(c)
	if !ok {
		return
	}
	group := c.DefaultQuery("status_group", "all")
	if !slices.Contains([]string{"all", "unpaid", "delivery", "delivered", "aftersale"}, group) {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	filter := model.ShopOrderFilter{Page: page, Limit: size, PublicID: c.Query("q"), StatusGroup: group}
	rows, total, e := model.ListShopOrderSummaries(shopDB(c), shopScope(c), filter)
	if e != nil {
		shopModelError(c, e)
		return
	}
	items := []gin.H{}
	for i := range rows {
		items = append(items, shopOrderDTO(&rows[i], shopScope(c).Admin))
	}
	shopSuccess(c, 200, shopPage(items, page, size, total))
}
func ShopCloseOrder(c *gin.Context)     { shopOrderAction(c, "close-order") }
func ShopReconcileOrder(c *gin.Context) { shopOrderAction(c, "reconcile-payment") }
func shopOrderAction(c *gin.Context, action string) {
	var in struct {
		ExpectedVersion string `json:"expected_version"`
	}
	if !shopBind(c, &in, 1024) {
		return
	}
	id := c.Param("order_no")
	if _, e := model.GetShopOrder(shopDB(c), shopScope(c), id); e != nil {
		shopModelError(c, e)
		return
	}
	cmd, ok := shopCommand(c, in, action, id, in.ExpectedVersion)
	if !ok {
		return
	}
	var e error
	status := 200
	if action == "close-order" {
		_, e = model.CloseShopOrder(shopDB(c), cmd, id, "buyer_closed")
	} else {
		_, e = model.RequestShopPaymentReconcile(shopDB(c), cmd, id)
		status = 202
	}
	if e != nil {
		shopModelError(c, e)
		return
	}
	shopRespondOrder(c, id, status)
}
func shopPaymentState(attempt model.ShopPaymentAttempt) string {
	if attempt.CreateState == "paid" {
		return "paid"
	}
	if attempt.CloseState == "closed" && attempt.CloseEvidenceRef != "" {
		if attempt.CreateState == "failed" {
			return "failed"
		}
		return "closed"
	}
	switch attempt.CreateState {
	case "prepared", "creating":
		return "creating"
	case "ready":
		return "ready"
	default:
		return "unknown"
	}
}

func ShopCreatePayment(c *gin.Context) {
	var in struct {
		ExpectedVersion string `json:"expected_version"`
		PaymentMethod   string `json:"payment_method"`
	}
	if !shopBind(c, &in, 1024) {
		return
	}
	id := c.Param("order_no")
	if _, e := model.GetShopOrder(shopDB(c), shopScope(c), id); e != nil {
		shopModelError(c, e)
		return
	}
	cmd, ok := shopCommand(c, in, "payment-attempt", id, in.ExpectedVersion)
	if !ok {
		return
	}
	r, e := service.CreateShopPayment(c.Request.Context(), shopDB(c), cmd, id, in.PaymentMethod)
	if e != nil {
		shopModelError(c, e)
		return
	}
	d, e := model.GetShopOrderDetail(shopDB(c), shopScope(c), id)
	if e != nil {
		shopModelError(c, e)
		return
	}
	// A callback may have committed since the payment service returned.
	for _, attempt := range d.Attempts {
		if attempt.ID == r.Attempt.ID {
			*r.Attempt = attempt
			break
		}
	}
	state := shopPaymentState(*r.Attempt)
	var checkout any
	if r.Checkout != nil && state == "ready" && d.Order.PaymentState != "paid" && d.Order.PurchaseState == "open" && !d.Order.RiskHold && d.Order.ReservationExpiresAt > time.Now().Unix() {
		checkout = gin.H{"kind": r.Checkout.Kind, "url": r.Checkout.URL, "expires_at": shopTime(r.Attempt.ProviderExpiresAt)}
	}
	status := 200
	if state == "creating" || state == "unknown" {
		status = 202
	}
	shopSuccess(c, status, gin.H{"order": shopOrderDTO(d, false), "attempt": gin.H{"id": r.Attempt.PublicID, "state": state, "expires_at": shopTime(r.Attempt.ProviderExpiresAt), "last_checked_at": shopTime(r.Attempt.LastCheckedAt)}, "checkout": checkout, "poll_after_seconds": 5})
}
func ShopGetBuyerInput(c *gin.Context) {
	id := c.Param("order_no")
	d, e := model.GetShopOrderDetail(shopDB(c), shopScope(c), id)
	if e != nil {
		shopModelError(c, e)
		return
	}
	scope := shopScope(c)
	values, e := service.ReadShopBuyerInput(c.Request.Context(), shopDB(c), model.ShopCommand{ActorID: scope.UserID, TenantID: scope.TenantID, Admin: scope.Admin, Now: time.Now().Unix()}, id)
	if e != nil {
		shopModelError(c, e)
		return
	}
	fields := d.Snapshot.Terms.BuyerInputSchema
	if fields == nil {
		fields = []model.ShopField{}
	}
	if values == nil {
		values = map[string]string{}
	}
	shopSuccess(c, 200, gin.H{"fields": fields, "values": values})
}
func ShopUpdateBuyerInput(c *gin.Context) {
	var in struct {
		ExpectedVersion string            `json:"expected_version"`
		Values          map[string]string `json:"values"`
	}
	if !shopBind(c, &in, 8192) {
		return
	}
	id := c.Param("order_no")
	d, e := model.GetShopOrderDetail(shopDB(c), shopScope(c), id)
	if e != nil {
		shopModelError(c, e)
		return
	}
	cmd, ok := shopCommand(c, in, "buyer-input", id, in.ExpectedVersion)
	if !ok {
		return
	}
	previous, e := model.GetShopIdempotency(shopDB(c), shopScope(c), "buyer-input", id, cmd.Key)
	if e != nil {
		shopModelError(c, e)
		return
	}
	if previous != nil {
		shopRespondOrder(c, id, 200)
		return
	}
	secret, keys, e := service.EncryptShopBuyerInput(id, d.Snapshot.Terms, in.Values)
	if e != nil {
		shopModelError(c, e)
		return
	}
	_, e = model.UpdateShopBuyerInput(shopDB(c), model.ShopBuyerInputUpdate{Command: cmd, OrderPublicID: id, Secret: secret, InputKeys: keys})
	if e != nil {
		shopModelError(c, e)
		return
	}
	shopRespondOrder(c, id, 200)
}
func ShopRevealOrder(c *gin.Context) {
	var in struct {
		DeliveryID string `json:"delivery_id"`
		Intent     string `json:"intent"`
		FieldKey   string `json:"field_key,omitempty"`
	}
	if !shopBind(c, &in, 2048) {
		return
	}
	if in.Intent == "copy" && in.FieldKey == "" || in.Intent == "view" && in.FieldKey != "" {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	scope := shopScope(c)
	r, e := service.RevealShopDelivery(c.Request.Context(), shopDB(c), model.ShopRevealInput{Command: model.ShopCommand{ActorID: scope.UserID, TenantID: scope.TenantID, Now: time.Now().Unix()}, OrderPublicID: c.Param("order_no"), DeliveryPublicID: in.DeliveryID, Intent: in.Intent}, in.FieldKey)
	if e != nil {
		shopModelError(c, e)
		return
	}
	shopSuccess(c, 200, gin.H{"order_no": r.Order.PublicID, "delivery_id": r.Delivery.PublicID, "delivery_version": shopID(r.Delivery.Version), "view_expires_at": shopTime(r.Delivery.ViewExpiresAt), "server_time": shopTime(time.Now().Unix()), "fields": r.Fields})
}
func ShopOrderEvents(c *gin.Context) {
	page, size, ok := shopPagination(c)
	if !ok {
		return
	}
	rows, total, e := model.ListShopOrderEvents(shopDB(c), shopScope(c), c.Param("order_no"), page, size)
	if e != nil {
		shopModelError(c, e)
		return
	}
	items := []gin.H{}
	for _, v := range rows {
		items = append(items, shopEventDTO(v))
	}
	shopSuccess(c, 200, shopPage(items, page, size, total))
}
