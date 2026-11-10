package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

func TestRefreshLoginSessionPreservesTemporaryLookupErrors(t *testing.T) {
	lookupFailure := errors.New("session database unavailable")
	for _, test := range []struct {
		name       string
		lookupErr  error
		wantErr    error
		wantStatus int
		wantCode   string
	}{
		{"unavailable", lookupFailure, lookupFailure, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR"},
		{"timeout", context.DeadlineExceeded, context.DeadlineExceeded, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR"},
		{"missing session", gorm.ErrRecordNotFound, ErrRefreshTokenInvalid, http.StatusUnauthorized, "AUTH_UNAUTHORIZED"},
		{"wrapped missing session", fmt.Errorf("session lookup: %w", gorm.ErrRecordNotFound), ErrRefreshTokenInvalid, http.StatusUnauthorized, "AUTH_UNAUTHORIZED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Inject the database result before any SQL executes; no database
			// server or schema is needed to exercise the public refresh flow.
			db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
			require.NoError(t, err)
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:session_lookup_error", func(tx *gorm.DB) {
				tx.AddError(test.lookupErr)
			}))
			previousDB, previousRedis := model.DB, common.RedisEnabled
			model.DB, common.RedisEnabled = db, false
			t.Cleanup(func() {
				model.DB, common.RedisEnabled = previousDB, previousRedis
			})

			bundle, user, err := RefreshLoginSession("93809a85-29ce-4541-84fa-ab6d85428eb8.refresh-secret", "", "127.0.0.1", "test-agent")

			require.ErrorIs(t, err, test.wantErr)
			assert.Nil(t, bundle)
			assert.Nil(t, user)
			status, code := AuthSessionErrorCode(err)
			assert.Equal(t, test.wantStatus, status)
			assert.Equal(t, test.wantCode, code)
			if test.wantStatus == http.StatusInternalServerError {
				assert.NotErrorIs(t, err, ErrRefreshTokenInvalid)
				assert.NotErrorIs(t, err, ErrLoginSessionRevoked)
			}
		})
	}
}
