package service

import (
	"sync/atomic"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type refundCountingFunding struct {
	refunds atomic.Int32
}

func (f *refundCountingFunding) Source() string       { return BillingSourceWallet }
func (f *refundCountingFunding) PreConsume(int) error { return nil }
func (f *refundCountingFunding) Settle(int) error     { return nil }
func (f *refundCountingFunding) Refund() error {
	f.refunds.Add(1)
	return nil
}

func TestBillingSessionRefundCompletesSynchronouslyExactlyOnce(t *testing.T) {
	funding := &refundCountingFunding{}
	session := &BillingSession{
		relayInfo:     &relaycommon.RelayInfo{IsPlayground: true},
		funding:       funding,
		tokenConsumed: 1,
	}
	c, _ := gin.CreateTestContext(nil)

	session.Refund(c)
	session.Refund(c)

	require.Equal(t, int32(1), funding.refunds.Load())
}
