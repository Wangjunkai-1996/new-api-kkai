package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
)

var ErrEpayQueryBudgetUnavailable = errors.New("epay merchant query budget unavailable")

// WaitEpayQueryBudget shares one query slot per database second across wallet,
// invoice and membership workers, including workers on different processes.
// The existing database-clock lease expires naturally; releasing it after the
// HTTP request would allow short requests to bypass the merchant budget.
func WaitEpayQueryBudget(ctx context.Context, db *gorm.DB, address, merchantID string) error {
	base, err := url.Parse(address)
	if ctx == nil || db == nil || err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || strings.TrimSpace(merchantID) == "" {
		return ErrEpayQueryBudgetUnavailable
	}
	identity := sha256.Sum256([]byte(strings.ToLower(base.Host) + "\x00" + strings.TrimRight(base.Path, "/") + "\x00" + merchantID))
	store := NewKKAILeaderLeaseStore(db, "epay-query:"+hex.EncodeToString(identity[:]))
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		acquired, err := store.TryAcquire(bounded, "merchant-query", time.Second)
		if err != nil {
			return ErrEpayQueryBudgetUnavailable
		}
		if acquired {
			return nil
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-bounded.Done():
			timer.Stop()
			return ErrEpayQueryBudgetUnavailable
		case <-timer.C:
		}
	}
}
