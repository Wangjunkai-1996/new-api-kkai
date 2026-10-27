package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

type membershipEligibility struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

type membershipTierView struct {
	model.MembershipTier
	Eligibility    membershipEligibility     `json:"eligibility"`
	PaymentMethods []membershipPaymentMethod `json:"payment_methods"`
}

type membershipOrderView struct {
	ID                int64                   `json:"id"`
	TradeNo           string                  `json:"trade_no"`
	UserID            int                     `json:"user_id,omitempty"`
	TierID            string                  `json:"tier_id"`
	TierName          string                  `json:"tier_name"`
	PriceMinor        int64                   `json:"price_minor"`
	AmountMinor       int64                   `json:"amount_minor"`
	Currency          string                  `json:"currency"`
	Provider          string                  `json:"provider,omitempty"`
	PaymentMethod     string                  `json:"payment_method,omitempty"`
	Quota             int64                   `json:"quota"`
	Action            string                  `json:"action"`
	Status            string                  `json:"status"`
	AccountID         int64                   `json:"account_id,omitempty"`
	CreatedAt         int64                   `json:"created_at"`
	PaidAt            int64                   `json:"paid_at,omitempty"`
	ExpiresAt         int64                   `json:"expires_at,omitempty"`
	ReceiptID         string                  `json:"receipt_id,omitempty"`
	ActualAmountMinor int64                   `json:"actual_amount_minor,omitempty"`
	ActualCurrency    string                  `json:"actual_currency,omitempty"`
	ActualProvider    string                  `json:"actual_provider,omitempty"`
	ActualOneTime     bool                    `json:"actual_one_time,omitempty"`
	ResolvedAt        int64                   `json:"resolved_at,omitempty"`
	CheckoutState     string                  `json:"checkout_state,omitempty"`
	ExceptionReason   string                  `json:"exception_reason,omitempty"`
	ResolutionNote    string                  `json:"resolution_note,omitempty"`
	Payments          []membershipPaymentView `json:"payments,omitempty"`
}

type membershipPaymentView struct {
	ReceiptID   string `json:"receipt_id"`
	Provider    string `json:"provider"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	OneTime     bool   `json:"one_time"`
}

func membershipOrderResponse(order *model.MembershipOrder, admin bool) (*membershipOrderView, error) {
	if order == nil {
		return nil, nil
	}
	var terms model.MembershipOrderTerms
	if err := common.UnmarshalJsonStr(order.Terms, &terms); err != nil {
		return nil, err
	}
	view := &membershipOrderView{ID: order.ID, TradeNo: order.TradeNo, TierID: order.TierID,
		TierName: terms.Tier.Name, PriceMinor: order.AmountMinor, AmountMinor: order.AmountMinor, Currency: order.Currency,
		Quota: terms.Tier.Quota, Action: terms.Action, Status: order.Status, AccountID: order.AccountID,
		CreatedAt: order.CreatedAt, PaidAt: order.PaidAt, CheckoutState: order.CheckoutState}
	if !admin {
		return view, nil
	}
	view.UserID = order.UserID
	view.Provider = order.Provider
	view.PaymentMethod = order.PaymentMethod
	view.ExpiresAt = order.ExpiresAt
	view.ReceiptID = order.ReceiptID
	view.ActualAmountMinor = order.ActualAmountMinor
	view.ActualCurrency = order.ActualCurrency
	view.ActualProvider = order.ActualProvider
	view.ActualOneTime = order.ActualOneTime
	view.ResolvedAt = order.ResolvedAt
	view.ExceptionReason = order.ExceptionReason
	if order.Resolution != "" {
		var resolution struct {
			Reason string `json:"reason"`
		}
		if err := common.UnmarshalJsonStr(order.Resolution, &resolution); err == nil {
			view.ResolutionNote = strings.TrimSpace(resolution.Reason)
		}
	}
	if len(order.Payments) > 0 {
		view.Payments = make([]membershipPaymentView, 0, len(order.Payments))
		for _, payment := range order.Payments {
			view.Payments = append(view.Payments, membershipPaymentView{ReceiptID: payment.ReceiptID,
				Provider: payment.Provider, Status: payment.Status, Reason: payment.Reason,
				AmountMinor: payment.AmountMinor, Currency: payment.Currency, OneTime: payment.OneTime})
		}
	}
	return view, nil
}

func membershipAPIError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	code := "membership_invalid_request"
	message := "Membership request is invalid."
	switch {
	case errors.Is(err, model.ErrMembershipInvalidInput):
		// Keep validation failures generic; details can contain provider or
		// catalog internals that are not part of the public API contract.
	case errors.Is(err, model.ErrMembershipDisabled):
		status, code = http.StatusServiceUnavailable, "membership_disabled"
		message = "New membership purchases are temporarily unavailable. Existing benefits remain valid."
	case errors.Is(err, model.ErrMembershipSchemaNotReady):
		status, code = http.StatusServiceUnavailable, "membership_unavailable"
		message = "Membership information could not be loaded. Please refresh before purchasing."
	case errors.Is(err, model.ErrMembershipConflict), errors.Is(err, model.ErrMembershipQuoteChanged):
		status, code = http.StatusConflict, "membership_state_changed"
		message = "Payment eligibility has changed. Review the selected orders again."
	case errors.Is(err, model.ErrMembershipPaymentReview):
		status, code = http.StatusConflict, "membership_payment_review"
		message = "Payment is recorded. An administrator will review this order; do not pay again."
	case errors.Is(err, model.ErrMembershipNotFound):
		status, code = http.StatusNotFound, "membership_not_found"
		message = "Content not found."
	default:
		status, code = http.StatusInternalServerError, "membership_internal_error"
		message = "Please try again later."
		logger.LogError(c.Request.Context(), fmt.Sprintf("membership request failed path=%s error=%v", c.Request.URL.Path, err))
	}
	c.JSON(status, gin.H{"success": false, "message": message, "code": code})
}

func GetMembershipCatalog(c *gin.Context) {
	if !model.MembershipEnabled() {
		common.ApiSuccess(c, gin.H{"runtime_enabled": false, "sales_enabled": false, "release_id": 0, "tiers": []membershipTierView{}, "groups": []model.MembershipGroup{}, "group_quotes": []service.MembershipCatalogGroupQuote{}})
		return
	}
	catalog, err := model.GetMembershipCatalog()
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	self, err := model.GetMembershipSelf(c.GetInt("id"))
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	allowed := service.GetUserUsableGroups(user.Group)
	groups := make([]model.MembershipGroup, 0, len(catalog.Groups))
	for _, group := range catalog.Groups {
		if _, ok := allowed[group.Key]; ok {
			groups = append(groups, group)
		}
	}
	tiers := make([]membershipTierView, 0, len(catalog.Tiers))
	for _, tier := range catalog.Tiers {
		if !tier.Enabled {
			continue
		}
		methods := membershipPaymentMethods(tier)
		eligibility := membershipEligibility{Action: "purchase"}
		if self.Account != nil && self.Account.ActiveAt(self.ServerTime) {
			switch {
			case tier.ID == self.Account.TierID:
				eligibility = membershipEligibility{Action: "current", Reason: "current_tier"}
			case tier.Rank <= self.Account.Rank:
				eligibility = membershipEligibility{Action: "unavailable", Reason: "downgrade_not_available"}
			default:
				eligibility.Action = "upgrade"
			}
		}
		if !catalog.SalesEnabled {
			eligibility = membershipEligibility{Action: "unavailable", Reason: "sales_closed"}
		}
		if len(methods) == 0 {
			eligibility = membershipEligibility{Action: "unavailable", Reason: "payment_unavailable"}
		}
		// Expose only the caller's accessible group coefficients.
		discounts := make(map[string]float64)
		for key, coefficient := range tier.Discounts {
			if _, ok := allowed[key]; ok {
				discounts[key] = coefficient
			}
		}
		tier.Discounts = discounts
		tier.PaymentProducts = nil
		tiers = append(tiers, membershipTierView{MembershipTier: tier, Eligibility: eligibility, PaymentMethods: methods})
	}
	quotes, err := service.GetMembershipCatalogGroupQuotesForUser(user, catalog)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"runtime_enabled": true, "release_id": catalog.ReleaseID, "sales_enabled": catalog.SalesEnabled, "tiers": tiers, "groups": groups, "group_quotes": quotes})
}

func GetMembershipSelf(c *gin.Context) {
	if !model.MembershipEnabled() {
		common.ApiSuccess(c, gin.H{"runtime_enabled": false, "server_time": time.Now().Unix(), "account": nil, "tier": nil, "discounts": []service.MembershipGroupQuote{}, "pending_order": nil, "available_quota": 0})
		return
	}
	userID := c.GetInt("id")
	self, err := model.GetMembershipSelf(userID)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	catalog, err := model.GetMembershipCatalog()
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	quotes, err := service.GetMembershipGroupQuotes(userID, catalog)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	orders, err := model.ListMembershipOrders(userID, model.MembershipOrderPending, 1)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	var pending *membershipOrderView
	if len(orders) > 0 {
		pending, err = membershipOrderResponse(&orders[0], false)
		if err != nil {
			membershipAPIError(c, err)
			return
		}
	}
	available := int64(0)
	if self.Account != nil && self.Account.ActiveAt(self.ServerTime) {
		available = self.Account.Available()
	}
	if self.Tier != nil {
		self.Tier.PaymentProducts = nil
		discounts := make(map[string]float64, len(quotes))
		for _, quote := range quotes {
			discounts[quote.GroupKey] = quote.Coefficient
		}
		self.Tier.Discounts = discounts
	}
	common.ApiSuccess(c, gin.H{"runtime_enabled": true, "server_time": self.ServerTime, "account": self.Account, "tier": self.Tier, "discounts": quotes, "pending_order": pending, "available_quota": available})
}

func GetMembershipOrders(c *gin.Context)      { listMembershipOrders(c, c.GetInt("id")) }
func AdminGetMembershipOrders(c *gin.Context) { listMembershipOrders(c, 0) }

func listMembershipOrders(c *gin.Context, userID int) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	orders, err := model.ListMembershipOrders(userID, c.Query("status"), limit)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	views := make([]membershipOrderView, 0, len(orders))
	for i := range orders {
		view, err := membershipOrderResponse(&orders[i], userID == 0)
		if err != nil {
			membershipAPIError(c, err)
			return
		}
		views = append(views, *view)
	}
	common.ApiSuccess(c, views)
}

func GetMembershipOrder(c *gin.Context) {
	order, err := model.GetMembershipOrder(c.Param("trade_no"), c.GetInt("id"))
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	view, err := membershipOrderResponse(order, false)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func CreateMembershipOrder(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	var input struct {
		TierID            string `json:"tier_id"`
		ReleaseID         int64  `json:"release_id"`
		ExpectedAccountID int64  `json:"expected_account_id"`
		ExpectedRevision  int64  `json:"expected_revision"`
		Provider          string `json:"payment_provider"`
		Method            string `json:"payment_method"`
	}
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		membershipAPIError(c, model.ErrMembershipInvalidInput)
		return
	}
	catalog, err := model.GetMembershipCatalog()
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	if input.ReleaseID <= 0 || input.ReleaseID != catalog.ReleaseID {
		membershipAPIError(c, model.ErrMembershipQuoteChanged)
		return
	}
	var tier *model.MembershipTier
	for i := range catalog.Tiers {
		if catalog.Tiers[i].ID == input.TierID {
			tier = &catalog.Tiers[i]
			break
		}
	}
	if tier == nil || !tier.Enabled {
		membershipAPIError(c, model.ErrMembershipNotFound)
		return
	}
	permitted := false
	for _, method := range membershipPaymentMethods(*tier) {
		if method.Provider == input.Provider && method.Method == input.Method {
			permitted = true
			break
		}
	}
	if !permitted {
		membershipAPIError(c, model.ErrMembershipInvalidInput)
		return
	}
	providerEnvironment := ""
	providerProductID := ""
	providerStoreID := ""
	switch input.Provider {
	case model.PaymentProviderStripe:
		providerEnvironment = membershipStripeEnvironment(setting.StripeApiSecret)
	case model.PaymentProviderCreem:
		providerEnvironment = "prod"
		if setting.CreemTestMode {
			providerEnvironment = "test"
		}
		providerProductID = tier.PaymentProducts[model.PaymentProviderCreem]
	case model.PaymentProviderWaffoPancake:
		// The membership Pancake client currently targets the production API.
		providerEnvironment = "prod"
		providerProductID = tier.PaymentProducts[model.PaymentProviderWaffoPancake]
		providerStoreID = strings.TrimSpace(setting.WaffoPancakeStoreID)
	}
	userID := c.GetInt("id")
	order, err := model.CreateMembershipOrder(model.MembershipOrderInput{
		UserID: userID, TierID: input.TierID, ReleaseID: catalog.ReleaseID, Provider: input.Provider, PaymentMethod: input.Method,
		ExpectedAccountID: input.ExpectedAccountID, ExpectedRevision: input.ExpectedRevision,
		ProviderEnvironment: providerEnvironment, ProviderProductID: providerProductID, ProviderStoreID: providerStoreID,
		TradeNo: fmt.Sprintf("MEMBER-%d-%s", userID, common.GetRandomString(24)), ExpiresAt: time.Now().Add(45 * time.Minute).Unix(),
	})
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	checkout, err := obtainMembershipCheckout(c.Request.Context(), order)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	view, err := membershipOrderResponse(order, false)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"order": view, "trade_no": order.TradeNo, "status": order.Status, "payment_url": checkout.PaymentURL, "submit_url": checkout.SubmitURL, "form_fields": checkout.FormFields})
}

func AdminGetMembershipConfig(c *gin.Context) {
	catalog, err := model.GetMembershipCatalog()
	if err != nil {
		if !model.MembershipEnabled() && model.DB != nil && errors.Is(err, model.ErrMembershipSchemaNotReady) {
			common.ApiSuccess(c, gin.H{"runtime_enabled": false, "schema_ready": false, "sales_enabled": false, "topup_discounts_disabled": false, "release_id": 0, "model_limits": map[string]model.MembershipModelLimit{}, "tiers": []model.MembershipTier{}, "groups": []model.MembershipGroup{}})
			return
		}
		membershipAPIError(c, err)
		return
	}
	known := make(map[string]bool, len(catalog.Groups))
	for _, group := range catalog.Groups {
		known[group.Key] = true
	}
	for key := range ratio_setting.GetGroupRatioCopy() {
		if !known[key] {
			catalog.Groups = append(catalog.Groups, model.MembershipGroup{Key: key, DisplayName: setting.GetGroupDisplayName(key), Enabled: true})
		}
	}
	common.ApiSuccess(c, struct {
		model.MembershipCatalog
		RuntimeEnabled bool `json:"runtime_enabled"`
		SchemaReady    bool `json:"schema_ready"`
	}{MembershipCatalog: *catalog, RuntimeEnabled: model.MembershipEnabled(), SchemaReady: true})
}

func AdminPublishMembershipConfig(c *gin.Context) {
	var input struct {
		Catalog           model.MembershipCatalog `json:"catalog"`
		ExpectedReleaseID int64                   `json:"expected_release_id"`
	}
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		membershipAPIError(c, model.ErrMembershipInvalidInput)
		return
	}
	if input.Catalog.SalesEnabled && !requirePaymentCompliance(c) {
		return
	}
	baseGroups := ratio_setting.GetGroupRatioCopy()
	for _, group := range input.Catalog.Groups {
		if _, ok := baseGroups[group.Key]; !ok && group.Enabled {
			membershipAPIError(c, model.ErrMembershipInvalidInput)
			return
		}
	}
	published, err := model.PublishMembershipCatalog(input.Catalog, input.ExpectedReleaseID, c.GetInt("id"))
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	common.ApiSuccess(c, struct {
		model.MembershipCatalog
		RuntimeEnabled bool `json:"runtime_enabled"`
		SchemaReady    bool `json:"schema_ready"`
	}{MembershipCatalog: *published, RuntimeEnabled: model.MembershipEnabled(), SchemaReady: true})
}

func AdminResolveMembershipOrder(c *gin.Context) {
	var input struct {
		OperationID string `json:"operation_id"`
		Action      string `json:"action"`
		Reason      string `json:"reason"`
	}
	if err := common.DecodeJson(c.Request.Body, &input); err != nil || strings.TrimSpace(input.Reason) == "" {
		membershipAPIError(c, model.ErrMembershipInvalidInput)
		return
	}
	order, err := model.ResolveMembershipOrder(c.Param("trade_no"), c.GetInt("id"), input.OperationID, input.Action, input.Reason)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	view, err := membershipOrderResponse(order, true)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}
