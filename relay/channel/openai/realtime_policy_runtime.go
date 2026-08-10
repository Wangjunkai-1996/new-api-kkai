package openai

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gorilla/websocket"
)

const realtimePolicyDrainTimeout = 5 * time.Second

func sendRealtimeRelayError(ch chan<- error, err error) {
	if err == nil {
		return
	}
	select {
	case ch <- err:
	default:
	}
}

func stopRealtimeReader(conn *websocket.Conn, done <-chan struct{}) {
	select {
	case <-done:
		return
	default:
	}
	if conn != nil {
		_ = conn.SetReadDeadline(time.Now())
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
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
