package channel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonpkg "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func membershipTransportFixture(t *testing.T) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	previousDB := model.DB
	mainDB, logDB := commonpkg.MainDatabaseType(), commonpkg.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.AutoMigrate(model.MembershipModels()...))
	model.DB = db
	commonpkg.SetDatabaseTypes(commonpkg.DatabaseTypeSQLite, commonpkg.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		commonpkg.SetDatabaseTypes(mainDB, logDB)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "transport-member", Quota: 420}).Error)
	require.NoError(t, db.Create(&model.MembershipBillingRecord{RequestID: "transport-call", Scope: model.MembershipBillingScopeRelay, UserID: 7, Source: model.MembershipWalletSource, ReservedQuota: 80, State: "reserved", CreatedAt: commonpkg.GetTimestamp()}).Error)
	service.InitHttpClient()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody)
	info := &relaycommon.RelayInfo{RequestId: "transport-call", MembershipBillingRequestID: "transport-call", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 11}}
	return ctx, info
}

func TestMembershipTransportRequiresStructuredZeroUsageRejection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		rejected bool
	}{
		{"rate_limit", 429, `{"error":{"type":"rate_limit_error","message":"try later"}}`, true},
		{"invalid_request", 400, `{"error":{"type":"invalid_request_error","message":"bad field"}}`, true},
		{"gemini_validation", 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"bad field"}}`, true},
		{"gateway_timeout", 504, `{"error":{"type":"rate_limit_error"}}`, false},
		{"upstream_execution_failure", 500, `{"error":{"type":"server_error"},"usage":{"total_tokens":21}}`, false},
		{"nominal_validation_with_usage", 400, `{"error":{"type":"invalid_request_error","details":{"usage":{"input_tokens":21}}}}`, false},
		{"nominal_limit_with_output", 429, `{"error":{"type":"rate_limit_error"},"output":[{"text":"finished"}]}`, false},
		{"unstructured_rejection", 403, `forbidden`, false},
		{"unknown_error_type", 400, `{"error":{"type":"execution_error"}}`, false},
		{"redirect", 307, `{"error":{"type":"invalid_request_error"}}`, false},
		{"oversized_rejection", 429, `{"error":{"type":"rate_limit_error","message":"` + strings.Repeat("x", 64<<10) + `"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, info := membershipTransportFixture(t)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(upstream.Close)
			request, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader(`{"model":"test"}`))
			require.NoError(t, err)
			response, err := doRequest(ctx, request, info)
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			assert.Equal(t, tc.body, string(body), "classification must preserve the original policy/error payload")
			assert.Equal(t, !tc.rejected, info.BillingOutcomeUnknown)
			record, err := model.GetMembershipBillingRecord(info.RequestId)
			require.NoError(t, err)
			assert.Equal(t, "reserved", record.State)
			assert.EqualValues(t, 80, record.ReservedQuota)
			if tc.rejected {
				assert.Zero(t, record.DispatchedAt)
				assert.Zero(t, record.ReviewChannelID)
			} else {
				assert.NotZero(t, record.DispatchedAt)
				assert.Equal(t, 11, record.ReviewChannelID)
			}
			var user model.User
			require.NoError(t, model.DB.First(&user, 7).Error)
			assert.EqualValues(t, 420, user.Quota, "classifying a response cannot change reserved funds")
		})
	}
}

func TestMembershipTransportDistinguishesFailureBeforeAndAfterWrite(t *testing.T) {
	for _, afterWrite := range []bool{false, true} {
		name := "before_write"
		if afterWrite {
			name = "after_write"
		}
		t.Run(name, func(t *testing.T) {
			ctx, info := membershipTransportFixture(t)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				connection, _, err := w.(http.Hijacker).Hijack()
				if assert.NoError(t, err) {
					assert.NoError(t, connection.Close())
				}
			}))
			t.Cleanup(upstream.Close)
			if !afterWrite {
				upstream.Close()
			}
			request, err := http.NewRequest(http.MethodPost, upstream.URL, strings.NewReader(`{"model":"test"}`))
			require.NoError(t, err)
			response, err := doRequest(ctx, request, info)
			require.Error(t, err)
			assert.Nil(t, response)
			assert.Equal(t, afterWrite, info.BillingOutcomeUnknown)
			record, err := model.GetMembershipBillingRecord(info.RequestId)
			require.NoError(t, err)
			assert.Equal(t, "reserved", record.State)
			if afterWrite {
				assert.NotZero(t, record.DispatchedAt)
			} else {
				assert.Zero(t, record.DispatchedAt)
			}
		})
	}
}
