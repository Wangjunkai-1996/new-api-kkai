package controller

import (
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

func TestResellerCustomerQuota(t *testing.T) {
	db := resellerAdminFixture(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TenantMembership{}, &model.CustomerPaymentLot{}, &model.TopUp{}))
	require.NoError(t, db.Create(&[]model.User{
		{Id: 100, TenantID: 42, Username: "funded", AffCode: "funded", Quota: 1_250_000, UsedQuota: 1_000_000},
		{Id: 200, TenantID: 42, Username: "pending", AffCode: "pending"},
		{Id: 300, TenantID: 43, Username: "foreign", AffCode: "foreign", Quota: 9_000_000},
		{Id: 400, TenantID: 42, Username: "inactive", AffCode: "inactive"},
		{Id: 500, TenantID: 42, Username: "deleted", AffCode: "deleted", DeletedAt: gorm.DeletedAt{Time: time.Unix(1, 0), Valid: true}},
		{Id: 600, TenantID: 42, Username: "no-membership", AffCode: "no-membership"},
	}).Error)
	require.NoError(t, db.Create(&[]model.TenantMembership{
		{TenantID: 42, UserID: 7, Role: model.TenantMembershipRoleOwner, Status: model.TenantMembershipStatusActive},
		{TenantID: 42, UserID: 100, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusActive},
		{TenantID: 42, UserID: 200, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusActive},
		{TenantID: 42, UserID: 300, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusActive},
		{TenantID: 42, UserID: 400, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusDisabled},
		{TenantID: 42, UserID: 500, Role: model.TenantMembershipRoleCustomer, Status: model.TenantMembershipStatusActive},
	}).Error)
	require.NoError(t, db.Create(&[]model.TopUp{
		{Id: 1, TenantID: 42, UserId: 100, TradeNo: "paid-first", Status: common.TopUpStatusSuccess, Amount: 200, Money: 24, PaidMinor: 2400},
		{Id: 2, TenantID: 42, UserId: 100, TradeNo: "paid-refunded", Status: common.TopUpStatusSuccess, Amount: 100, Money: 17, PaidMinor: 1700},
		{Id: 3, TenantID: 42, UserId: 200, TradeNo: "awaiting-payment", Status: common.TopUpStatusPending, Amount: 999_999, Money: 777, PaidMinor: 77_700},
		{Id: 4, TenantID: 43, UserId: 100, TradeNo: "foreign-lot", Status: common.TopUpStatusSuccess, PaidMinor: 90_000},
	}).Error)
	require.NoError(t, db.Create(&[]model.CustomerPaymentLot{
		{TenantID: 42, CustomerID: 100, TopUpID: 1, OperationID: "topup:1", PaidMinor: 2400, CreditedQuota: 2_000_000, ConsumedQuota: 750_000, RemainingQuota: 1_250_000, RefundStatus: "none"},
		{TenantID: 42, CustomerID: 100, TopUpID: 2, OperationID: "topup:2", PaidMinor: 1700, CreditedQuota: 1_000_000, ConsumedQuota: 250_000, RefundedQuota: 750_000, RefundStatus: "refunded"},
		{TenantID: 43, CustomerID: 100, TopUpID: 4, OperationID: "topup:4", PaidMinor: 90_000, CreditedQuota: 9_000_000, ConsumedQuota: 7_000_000, RefundedQuota: 2_000_000, RefundStatus: "refunded"},
	}).Error)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 7)
		common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: 42})
	}, middleware.TenantAdminAuth())
	router.GET("/customers", GetResellerCustomers)

	for _, test := range []struct {
		name, query                     string
		page, customerID                int
		quota, used, credited, refunded int64
	}{
		{name: "successful credits retain refund history and quota units", query: "?p=1&page_size=1&tenant_id=43", page: 1, customerID: 100, quota: 1_250_000, used: 1_000_000, credited: 3_000_000, refunded: 750_000},
		{name: "pending payment credits nothing", query: "?p=2&page_size=1", page: 2, customerID: 200},
		{name: "empty page", query: "?p=3&page_size=1", page: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/customers"+test.query, nil))
			require.Equal(t, http.StatusOK, response.Code)
			var result struct {
				Success  bool `json:"success"`
				Total    int  `json:"total"`
				Page     int  `json:"page"`
				PageSize int  `json:"page_size"`
				Data     []struct {
					ID                 int    `json:"id"`
					Quota              *int64 `json:"quota"`
					UsedQuota          *int64 `json:"used_quota"`
					TotalCreditedQuota *int64 `json:"total_credited_quota"`
					TotalRefundedQuota *int64 `json:"total_refunded_quota"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			require.True(t, result.Success, response.Body.String())
			assert.Equal(t, 2, result.Total)
			assert.Equal(t, test.page, result.Page)
			assert.Equal(t, 1, result.PageSize)
			if test.customerID == 0 {
				assert.NotNil(t, result.Data)
				assert.Empty(t, result.Data)
				return
			}
			require.Len(t, result.Data, 1)
			customer := result.Data[0]
			assert.Equal(t, test.customerID, customer.ID)
			require.NotNil(t, customer.Quota)
			require.NotNil(t, customer.UsedQuota)
			require.NotNil(t, customer.TotalCreditedQuota)
			require.NotNil(t, customer.TotalRefundedQuota)
			assert.Equal(t, test.quota, *customer.Quota)
			assert.Equal(t, test.used, *customer.UsedQuota)
			assert.Equal(t, test.credited, *customer.TotalCreditedQuota)
			assert.Equal(t, test.refunded, *customer.TotalRefundedQuota)
		})
	}

	t.Run("customers cannot access other customers financial data", func(t *testing.T) {
		customerRouter := gin.New()
		customerRouter.Use(func(c *gin.Context) {
			c.Set("id", 100)
			common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: 42})
		}, middleware.TenantAdminAuth())
		customerRouter.GET("/customers", GetResellerCustomers)
		response := httptest.NewRecorder()
		customerRouter.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/customers", nil))
		assert.Equal(t, http.StatusForbidden, response.Code)
		var result struct {
			Success bool   `json:"success"`
			Code    string `json:"code"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.False(t, result.Success)
		assert.Equal(t, "TENANT_OWNER_REQUIRED", result.Code)
		assert.NotContains(t, response.Body.String(), "total_credited_quota")
	})

	t.Run("empty customer collection", func(t *testing.T) {
		require.NoError(t, db.Where("tenant_id = ? AND role = ?", 42, model.TenantMembershipRoleCustomer).Delete(&model.TenantMembership{}).Error)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/customers", nil))
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			Success bool  `json:"success"`
			Total   int   `json:"total"`
			Data    []any `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.True(t, result.Success, response.Body.String())
		assert.Zero(t, result.Total)
		assert.NotNil(t, result.Data)
		assert.Empty(t, result.Data)
	})
}
