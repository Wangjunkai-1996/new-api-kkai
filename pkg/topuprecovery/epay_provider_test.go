package topuprecovery

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestEPayProviderLoadsDatabaseConfigurationAndParsesSuccessTime(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/api.php", request.URL.Path)
		assert.Equal(t, "order", request.URL.Query().Get("act"))
		assert.Equal(t, "partner-id", request.URL.Query().Get("pid"))
		assert.Equal(t, "provider-key", request.URL.Query().Get("key"))
		assert.Equal(t, "trade-444", request.URL.Query().Get("out_trade_no"))
		_, _ = fmt.Fprint(response, `{"code":1,"status":1,"trade_no":"provider-1","out_trade_no":"trade-444","type":"alipay","endtime":"2026-07-14 16:00:28"}`)
	}))
	defer server.Close()
	provider, err := NewEPayProviderFromDatabase(newProviderDatabase(t, server.URL), server.Client())
	require.NoError(t, err)

	order, err := provider.Lookup(context.Background(), "trade-444")
	require.NoError(t, err)
	assert.Equal(t, "provider-1", order.TradeNo)
	assert.Equal(t, "trade-444", order.ServiceTradeNo)
	assert.Equal(t, time.Date(2026, 7, 14, 8, 0, 28, 0, time.UTC).Unix(), order.CompletedAt)
}

func TestEPayProviderRejectsRedirectsAndDoesNotLeakCredentialInErrors(t *testing.T) {
	redirectTargetCalled := false
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectTargetCalled = true
	}))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL, http.StatusFound)
	}))
	defer server.Close()
	provider, err := NewEPayProviderFromDatabase(newProviderDatabase(t, server.URL), server.Client())
	require.NoError(t, err)

	_, err = provider.Lookup(context.Background(), "trade-444")
	require.ErrorIs(t, err, ErrInvalidProviderEvidence)
	assert.NotContains(t, err.Error(), "provider-key")
	assert.False(t, redirectTargetCalled)
}

func TestEPayProviderRejectsUnpaidOrMismatchedOrders(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(response, `{"code":1,"status":0,"trade_no":"provider-1","out_trade_no":"other","type":"alipay","endtime":"2026-07-14 16:00:28"}`)
	}))
	defer server.Close()
	provider, err := NewEPayProviderFromDatabase(newProviderDatabase(t, server.URL), server.Client())
	require.NoError(t, err)

	_, err = provider.Lookup(context.Background(), "trade-444")
	require.ErrorIs(t, err, ErrInvalidProviderEvidence)
}

func newProviderDatabase(t *testing.T, address string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&optionRow{}))
	require.NoError(t, db.Create(&[]optionRow{
		{Key: "PayAddress", Value: address},
		{Key: "EpayId", Value: "partner-id"},
		{Key: "EpayKey", Value: "provider-key"},
	}).Error)
	return db
}
