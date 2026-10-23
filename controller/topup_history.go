package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// Legacy clients use status=pending to show manual completion. Project the
// site's ended attempts without mutating payment facts or mirrored purchases.
func walletLegacyHistory(topups []*model.TopUp) ([]any, error) {
	ids := make([]int, 0, len(topups))
	for _, order := range topups {
		ids = append(ids, order.Id)
	}
	var walletIDs []int
	if len(ids) > 0 {
		if err := model.WalletTopUpOnlyQuery(model.DB.Model(&model.TopUp{})).Where("id IN ?", ids).Pluck("id", &walletIDs).Error; err != nil {
			return nil, err
		}
	}
	walletOrders := make(map[int]bool, len(walletIDs))
	for _, id := range walletIDs {
		walletOrders[id] = true
	}
	now := time.Now().Unix()
	rows := make([]any, 0, len(topups))
	for _, order := range topups {
		ended := model.WalletTopUpLocalEnded(order, now)
		unknownTime := model.WalletTopUpDeadline(order) <= 0 && order.Status != common.TopUpStatusSuccess && order.Status != "credit_review" && order.ProviderPaymentState != "paid" && !model.WalletTopUpReliableTerminal(order)
		if !walletOrders[order.Id] || !ended && !unknownTime {
			rows = append(rows, order)
			continue
		}
		displayStatus, _, _ := walletOrderStatus(order)
		legacyStatus := common.TopUpStatusExpired
		if unknownTime && !ended {
			legacyStatus = "unavailable"
		}
		rows = append(rows, struct {
			*model.TopUp
			Status        string `json:"status"`
			DisplayStatus string `json:"display_status"`
		}{order, legacyStatus, displayStatus})
	}
	return rows, nil
}

// New filters select the explicit union; old callers retain their existing
// response and default time window during the frontend rollout.
func walletFilteredHistory(c *gin.Context, allUsers bool) bool {
	if c.Query("scope") == "" && c.Query("status_group") == "" && c.Query("exact_trade_no") == "" {
		return false
	}
	if !walletSchemaReady(c) {
		return true
	}
	page := common.GetPageQuery(c)
	if page.Page < 1 || page.PageSize < 1 || page.Page > 1_000_000 {
		walletError(c, http.StatusBadRequest, "VALIDATION_FAILED", nil)
		return true
	}
	scope := walletScope(c)
	if allUsers {
		scope = model.WalletTopUpScope{TenantID: -1}
		if raw := c.Query("user_id"); raw != "" {
			id, e := strconv.Atoi(raw)
			if e != nil || id < 1 {
				walletError(c, 400, "VALIDATION_FAILED", nil)
				return true
			}
			scope.UserID = id
		}
	}
	result, err := model.QueryWalletTopUps(model.WalletTopUpHistoryQuery{Scope: scope, AllUsers: allUsers, QueryScope: c.Query("scope"), StatusGroup: c.Query("status_group"), Keyword: c.Query("keyword"), ExactTradeNo: c.Query("exact_trade_no"), Offset: page.GetStartIdx(), Limit: page.PageSize})
	if err != nil {
		if err == model.ErrResellerFinanceInvalidInput {
			walletError(c, 400, "VALIDATION_FAILED", nil)
		} else {
			walletModelError(c, err)
		}
		return true
	}
	ids := make([]int, 0, len(result.Orders))
	for _, order := range result.Orders {
		ids = append(ids, order.Id)
	}
	receipts, err := model.GetWalletCreditedReceipts(ids)
	if err != nil {
		walletModelError(c, err)
		return true
	}
	type historyOrder struct {
		dto.WalletOrder
		UserID *int `json:"user_id,omitempty"`
	}
	orders := make([]historyOrder, 0, len(result.Orders))
	for _, order := range result.Orders {
		projected := WalletTopUpOrderDTO(order, false)
		if receipt, ok := receipts[order.Id]; ok {
			projected.ReceivedPayment = &dto.WalletMoney{AmountMinor: strconv.FormatInt(receipt.AmountMinor, 10), Currency: receipt.Currency, Exponent: receipt.CurrencyExponent}
		}
		row := historyOrder{WalletOrder: projected}
		if allUsers {
			id := order.UserId
			row.UserID = &id
		}
		orders = append(orders, row)
	}
	var userID *int
	if !allUsers {
		userID = &scope.UserID
	}
	now := time.Now()
	total, e := model.GetTopUpTodayPaymentTotal(userID, now)
	if e != nil {
		walletModelError(c, e)
		return true
	}
	page.SetTotal(int(result.Total))
	page.SetItems(orders)
	walletSuccess(c, 200, struct {
		*common.PageInfo
		Counts            map[string]int64 `json:"counts"`
		QueryScope        string           `json:"query_scope"`
		TodayPaymentTotal float64          `json:"today_payment_total"`
		TodayPaymentDate  string           `json:"today_payment_date"`
		ServerTime        int64            `json:"server_time"`
	}{page, result.Counts, result.QueryScope, total, now.Format("2006-01-02"), now.Unix()})
	return true
}
