package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
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
	model.MembershipOrder
	TierName   string `json:"tier_name"`
	PriceMinor int64  `json:"price_minor"`
	Quota      int64  `json:"quota"`
	Action     string `json:"action"`
}

func membershipOrderResponse(order *model.MembershipOrder) (*membershipOrderView, error) {
	if order == nil {
		return nil, nil
	}
	var terms model.MembershipOrderTerms
	if err := common.UnmarshalJsonStr(order.Terms, &terms); err != nil {
		return nil, err
	}
	return &membershipOrderView{MembershipOrder: *order, TierName: terms.Tier.Name,
		PriceMinor: order.AmountMinor, Quota: terms.Tier.Quota, Action: terms.Action}, nil
}

func membershipAPIError(c *gin.Context, err error) {
	status := http.StatusBadRequest
	code := "membership_invalid_request"
	switch {
	case errors.Is(err, model.ErrMembershipDisabled):
		status, code = http.StatusServiceUnavailable, "membership_disabled"
	case errors.Is(err, model.ErrMembershipSchemaNotReady):
		status, code = http.StatusServiceUnavailable, "membership_unavailable"
	case errors.Is(err, model.ErrMembershipConflict), errors.Is(err, model.ErrMembershipQuoteChanged):
		status, code = http.StatusConflict, "membership_state_changed"
	case errors.Is(err, model.ErrMembershipNotFound):
		status, code = http.StatusNotFound, "membership_not_found"
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error(), "code": code})
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
	quotes, err := service.GetMembershipCatalogGroupQuotes(user.Id, catalog)
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
		pending, err = membershipOrderResponse(&orders[0])
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
		view, err := membershipOrderResponse(&orders[i])
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
	view, err := membershipOrderResponse(order)
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
		membershipAPIError(c, fmt.Errorf("payment method is unavailable for this membership"))
		return
	}
	userID := c.GetInt("id")
	order, err := model.CreateMembershipOrder(model.MembershipOrderInput{
		UserID: userID, TierID: input.TierID, ReleaseID: catalog.ReleaseID, Provider: input.Provider, PaymentMethod: input.Method,
		ExpectedAccountID: input.ExpectedAccountID, ExpectedRevision: input.ExpectedRevision,
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
	view, err := membershipOrderResponse(order)
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
			common.ApiSuccess(c, gin.H{"runtime_enabled": false, "sales_enabled": false, "topup_discounts_disabled": false, "release_id": 0, "model_limits": map[string]model.MembershipModelLimit{}, "tiers": []model.MembershipTier{}, "groups": []model.MembershipGroup{}})
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
	common.ApiSuccess(c, catalog)
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
			membershipAPIError(c, fmt.Errorf("unknown enabled billing group: %s", group.Key))
			return
		}
	}
	published, err := model.PublishMembershipCatalog(input.Catalog, input.ExpectedReleaseID, c.GetInt("id"))
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	common.ApiSuccess(c, published)
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
	view, err := membershipOrderResponse(order)
	if err != nil {
		membershipAPIError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}
