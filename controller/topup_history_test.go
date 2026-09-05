package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTopUpHistoryTodayPaymentTotal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&model.TopUp{}))
		sqlDB, err := db.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1)
		previousDB := model.DB
		model.DB = db
		t.Cleanup(func() {
			model.DB = previousDB
			require.NoError(t, sqlDB.Close())
		})

		now := time.Now()
		orders := []model.TopUp{
			{UserId: 7, TradeNo: "match-first", Money: 15, Status: common.TopUpStatusSuccess, CreateTime: now.Unix(), CompleteTime: now.Unix()},
			{UserId: 7, TradeNo: "match-second", Money: 37.5, Status: common.TopUpStatusSuccess, CreateTime: now.Unix(), CompleteTime: now.Unix()},
			{UserId: 7, TradeNo: "created-last-month", Money: 8.25, Status: common.TopUpStatusSuccess, CreateTime: now.AddDate(0, 0, -31).Unix(), CompleteTime: now.Unix()},
			{UserId: 8, TradeNo: "another-user", Money: 20, Status: common.TopUpStatusSuccess, CreateTime: now.Unix(), CompleteTime: now.Unix()},
			{UserId: 7, TradeNo: "pending", Money: 500, Status: common.TopUpStatusPending, CreateTime: now.Unix()},
			{UserId: 0, TradeNo: "zero-user", Money: 1.25, Status: common.TopUpStatusSuccess, CreateTime: now.Unix(), CompleteTime: now.Unix()},
		}
		require.NoError(t, db.Create(&orders).Error)

		for _, tc := range []struct {
			name       string
			handler    gin.HandlerFunc
			userID     int
			query      string
			wantTotal  float64
			wantCount  int
			wantTrades []string
		}{
			{name: "user page", handler: GetUserTopUps, userID: 7, wantTotal: 60.75, wantCount: 3, wantTrades: []string{"match-second"}},
			{name: "user search", handler: GetUserTopUps, userID: 7, query: "&keyword=match%25", wantTotal: 60.75, wantCount: 2, wantTrades: []string{"match-first"}},
			{name: "user empty search", handler: GetUserTopUps, userID: 7, query: "&keyword=absent", wantTotal: 60.75},
			{name: "admin page", handler: GetAllTopUps, wantTotal: 82, wantCount: 6, wantTrades: []string{"pending"}},
			{name: "admin search", handler: GetAllTopUps, query: "&keyword=match%25", wantTotal: 82, wantCount: 2, wantTrades: []string{"match-first"}},
			{name: "admin empty search", handler: GetAllTopUps, query: "&keyword=absent", wantTotal: 82},
			{name: "zero user stays scoped", handler: GetUserTopUps, wantTotal: 1.25, wantCount: 1},
			{name: "user without payments", handler: GetUserTopUps, userID: 99},
		} {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Set("id", tc.userID)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/topup?p=2&page_size=1"+tc.query, nil)
			tc.handler(ctx)

			var response struct {
				Success bool `json:"success"`
				Data    struct {
					Page              int           `json:"page"`
					PageSize          int           `json:"page_size"`
					Total             int           `json:"total"`
					Items             []model.TopUp `json:"items"`
					TodayPaymentTotal *float64      `json:"today_payment_total"`
					TodayPaymentDate  string        `json:"today_payment_date"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), tc.name)
			require.True(t, response.Success, "%s: %s", tc.name, recorder.Body.String())
			assert.Equal(t, http.StatusOK, recorder.Code, tc.name)
			assert.Equal(t, 2, response.Data.Page, tc.name)
			assert.Equal(t, 1, response.Data.PageSize, tc.name)
			assert.Equal(t, tc.wantCount, response.Data.Total, tc.name)
			require.NotNil(t, response.Data.TodayPaymentTotal, tc.name)
			assert.Equal(t, tc.wantTotal, *response.Data.TodayPaymentTotal, tc.name)
			assert.Equal(t, now.Format("2006-01-02"), response.Data.TodayPaymentDate, tc.name)
			var trades []string
			for _, order := range response.Data.Items {
				trades = append(trades, order.TradeNo)
			}
			assert.Equal(t, tc.wantTrades, trades, tc.name)
		}
	})
}
