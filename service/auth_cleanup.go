package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const AuthArtifactCleanupInterval = time.Hour

// RunAuthArtifactCleanup is scheduled under the application's writer lease.
func RunAuthArtifactCleanup(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var cleanupErrors []error
	now := time.Now()
	count, err := model.CountUserSessionsCreatedSince(0, now.Add(-time.Hour).Unix())
	if err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("count hourly user session issuance: %w", err))
	} else if count > int64(common.UserSessionHourlyAlertThreshold) {
		common.SysError(fmt.Sprintf(
			"hourly user session issuance exceeded alert threshold: count=%d threshold=%d window_seconds=%d",
			count,
			common.UserSessionHourlyAlertThreshold,
			int64(time.Hour/time.Second),
		))
	}
	if err := model.DeleteExpiredUserSessions(now.Unix()); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("delete expired user sessions: %w", err))
	}
	if err := model.DeleteOldRevokedUserSessions(now.Unix()); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("delete old revoked user sessions: %w", err))
	}
	if err := model.DeleteExpiredAuthFlows(now); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("delete expired authentication flows: %w", err))
	}
	accessTokenExpiredBefore := now.Unix() - int64(common.UserSessionRevokedRetentionDays)*24*60*60
	if err := model.DeleteExpiredUserAccessTokens(accessTokenExpiredBefore); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("delete expired access tokens: %w", err))
	}
	return errors.Join(cleanupErrors...)
}
