package relay

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const (
	defaultImageSyncGlobalLimit  = 8
	defaultImageSyncAccountLimit = 4
)

type imageSyncAdmissionGate struct {
	mu           sync.Mutex
	globalLimit  int
	accountLimit int
	inFlight     int
	byAccount    map[string]int
}

type imageSyncLease struct {
	gate      *imageSyncAdmissionGate
	accountID string
	once      sync.Once
}

var activeImageSyncGate = newImageSyncAdmissionGate(
	common.GetEnvOrDefault("IMAGE_SYNC_GLOBAL_CONCURRENCY", defaultImageSyncGlobalLimit),
	common.GetEnvOrDefault("IMAGE_SYNC_ACCOUNT_CONCURRENCY", defaultImageSyncAccountLimit),
)

func newImageSyncAdmissionGate(globalLimit, accountLimit int) *imageSyncAdmissionGate {
	if globalLimit < 1 {
		globalLimit = defaultImageSyncGlobalLimit
	}
	if accountLimit < 1 {
		accountLimit = defaultImageSyncAccountLimit
	}
	return &imageSyncAdmissionGate{
		globalLimit:  globalLimit,
		accountLimit: accountLimit,
		byAccount:    make(map[string]int),
	}
}

func (g *imageSyncAdmissionGate) TryAcquire(accountID string) (*imageSyncLease, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight >= g.globalLimit || g.byAccount[accountID] >= g.accountLimit {
		return nil, false
	}
	g.inFlight++
	g.byAccount[accountID]++
	return &imageSyncLease{gate: g, accountID: accountID}, true
}

func (l *imageSyncLease) Release() {
	if l == nil || l.gate == nil {
		return
	}
	l.once.Do(func() {
		l.gate.release(l.accountID)
	})
}

func (g *imageSyncAdmissionGate) release(accountID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.inFlight--
	g.byAccount[accountID]--
	if g.byAccount[accountID] == 0 {
		delete(g.byAccount, accountID)
	}
}

func imageSyncAccountID(info *relaycommon.RelayInfo) string {
	if info == nil {
		return "unknown"
	}
	digest := sha256.Sum256([]byte(info.ApiKey))
	return fmt.Sprintf("%d:%x", info.ChannelId, digest[:8])
}
