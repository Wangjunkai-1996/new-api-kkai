package openai

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
)

const defaultRealtimePolicyDrainTimeout = 5 * time.Minute

func realtimePolicyDrainDuration() time.Duration {
	if constant.StreamingTimeout > 0 {
		return time.Duration(constant.StreamingTimeout) * time.Second
	}
	return defaultRealtimePolicyDrainTimeout
}

func sendRealtimeRelayError(ch chan<- error, err error) {
	if err == nil {
		return
	}
	select {
	case ch <- err:
	default:
	}
}

type realtimePolicyConnectionCloser interface {
	Close() error
}

func stopRealtimeReaders(clientConn, targetConn realtimePolicyConnectionCloser, clientDone, targetDone <-chan struct{}) {
	// A reader may be blocked writing to the opposite connection. Closing both
	// transports first releases either direction before waiting for termination.
	if clientConn != nil {
		_ = clientConn.Close()
	}
	if targetConn != nil {
		_ = targetConn.Close()
	}
	if clientDone != nil {
		<-clientDone
	}
	if targetDone != nil {
		<-targetDone
	}
}

func realtimePolicyError(event *dto.RealtimeEvent) *types.NewAPIError {
	if event == nil || event.Type != dto.RealtimeEventTypeError || event.Error == nil {
		return nil
	}
	structuredCode := fmt.Sprint(event.Error.Code)
	if service.KKAILocalPolicyCode(structuredCode) == "" &&
		service.KKAIPolicyMarkerEvidence(structuredCode+" "+event.Error.Message) == "" {
		return nil
	}
	return service.NewKKAIStructuredRelayError(event.Error)
}
