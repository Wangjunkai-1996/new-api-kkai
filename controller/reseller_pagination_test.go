package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResellerPaginationPreservesTenantScopeAndLifetimeProfit(t *testing.T) {
	db := resellerAdminFixture(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TenantMembership{}, &model.TenantPriceVersion{}, &model.ResellerWallet{}, &model.ResellerLedgerEntry{}, &model.PricingQuote{}, &model.CustomerPaymentLot{}, &model.ResellerWithdrawal{}, &model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))
	require.NoError(t, db.Create(&model.TenantMembership{TenantID: 42, UserID: 7, Role: model.TenantMembershipRoleOwner, Status: model.TenantMembershipStatusActive}).Error)
	require.NoError(t, db.Create(&model.ResellerWallet{TenantID: 42, Version: 1, ExactProfitCarry: "0"}).Error)
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)

	users := make([]model.User, 0, 205)
	memberships := make([]model.TenantMembership, 0, 204)
	ledger := make([]model.ResellerLedgerEntry, 0, 204)
	quotes := make([]model.PricingQuote, 0, 202)
	withdrawals := make([]model.ResellerWithdrawal, 0, 202)
	for index := 0; index < 201; index++ {
		userID := index + 100
		reference := fmt.Sprintf("pagination-%d", index)
		users = append(users, model.User{Id: userID, TenantID: 42, Username: reference, AffCode: reference})
		memberships = append(memberships, model.TenantMembership{TenantID: 42, UserID: userID, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusActive})
		ledger = append(ledger, model.ResellerLedgerEntry{TenantID: 42, OperationID: reference, Kind: model.ResellerLedgerKindSettlement, AmountMinor: 2})
		quotes = append(quotes, model.PricingQuote{TenantID: 42, RequestID: reference})
		withdrawals = append(withdrawals, model.ResellerWithdrawal{TenantID: 42, IdempotencyKey: reference})
	}
	users = append(users,
		model.User{Id: 9000, TenantID: 43, Username: "foreign", AffCode: "foreign"},
		model.User{Id: 9001, TenantID: 42, Username: "inactive", AffCode: "inactive"},
		model.User{Id: 9002, TenantID: 42, Username: "deleted", AffCode: "deleted", DeletedAt: gorm.DeletedAt{Time: time.Unix(1, 0), Valid: true}},
		model.User{Id: 9003, TenantID: 42, Username: "no-membership", AffCode: "no-membership"},
	)
	memberships = append(memberships,
		model.TenantMembership{TenantID: 42, UserID: 9000, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusActive},
		model.TenantMembership{TenantID: 42, UserID: 9001, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusDisabled},
		model.TenantMembership{TenantID: 42, UserID: 9002, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusActive},
	)
	ledger = append(ledger,
		model.ResellerLedgerEntry{TenantID: 42, OperationID: "chargeback", Kind: model.ResellerLedgerKindChargeback, AmountMinor: -1},
		model.ResellerLedgerEntry{TenantID: 42, OperationID: "usage", Kind: model.ResellerLedgerKindUsageReconciliation, AmountMinor: 1000},
		model.ResellerLedgerEntry{TenantID: 43, OperationID: "foreign", Kind: model.ResellerLedgerKindSettlement, AmountMinor: 1000},
	)
	quotes = append(quotes, model.PricingQuote{TenantID: 43, RequestID: "foreign"})
	withdrawals = append(withdrawals, model.ResellerWithdrawal{TenantID: 43, IdempotencyKey: "foreign"})
	require.NoError(t, db.Create(&users).Error)
	require.NoError(t, db.Create(&memberships).Error)
	require.NoError(t, db.Create(&ledger).Error)
	require.NoError(t, db.Create(&quotes).Error)
	require.NoError(t, db.Create(&withdrawals).Error)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 7)
		common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: 42})
	}, middleware.TenantAdminAuth())
	router.GET("/customers", GetResellerCustomers)
	router.GET("/earnings", GetResellerEarnings)
	router.GET("/withdrawals", GetResellerWithdrawals)

	for _, test := range []struct {
		path      string
		wantTotal int
		wantIDs   []int
	}{
		{"/customers", 201, []int{300}},
		{"/withdrawals", 201, []int{1}},
	} {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path+"?p=3&page_size=100", nil))
			var result struct {
				Success  bool               `json:"success"`
				Total    int                `json:"total"`
				Page     int                `json:"page"`
				PageSize int                `json:"page_size"`
				Data     []struct{ ID int } `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			require.True(t, result.Success, response.Body.String())
			assert.Equal(t, test.wantTotal, result.Total)
			assert.Equal(t, 3, result.Page)
			assert.Equal(t, 100, result.PageSize)
			ids := make([]int, 0, len(result.Data))
			for _, item := range result.Data {
				ids = append(ids, item.ID)
			}
			assert.Equal(t, test.wantIDs, ids)
		})
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/earnings?p=3&page_size=100", nil))
	var earnings struct {
		Success bool `json:"success"`
		Data    struct {
			Ledger           []model.ResellerLedgerEntry `json:"ledger"`
			LedgerTotal      int                         `json:"ledger_total"`
			Quotes           []model.PricingQuote        `json:"quotes"`
			QuotesTotal      int                         `json:"quotes_total"`
			TotalProfitMinor int64                       `json:"total_profit_minor"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &earnings))
	require.True(t, earnings.Success, response.Body.String())
	assert.Equal(t, 202, earnings.Data.LedgerTotal)
	require.Len(t, earnings.Data.Ledger, 2)
	assert.Equal(t, int64(2), earnings.Data.Ledger[0].ID)
	assert.Equal(t, int64(1), earnings.Data.Ledger[1].ID)
	assert.Equal(t, 201, earnings.Data.QuotesTotal)
	require.Len(t, earnings.Data.Quotes, 1)
	assert.Equal(t, int64(1), earnings.Data.Quotes[0].ID)
	assert.Equal(t, int64(401), earnings.Data.TotalProfitMinor)

	for _, query := range []string{"", "?p=-1&page_size=-1", "?p=9223372036854775807&page_size=100"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/customers"+query, nil))
		var result struct {
			Success  bool         `json:"success"`
			Page     int          `json:"page"`
			PageSize int          `json:"page_size"`
			Data     []model.User `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		require.True(t, result.Success, response.Body.String())
		assert.Equal(t, 1, result.Page)
		assert.Positive(t, result.PageSize)
		if query == "" {
			assert.Len(t, result.Data, 200)
			assert.Equal(t, 200, result.PageSize)
		}
	}
}
