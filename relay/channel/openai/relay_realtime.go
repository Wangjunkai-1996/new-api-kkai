package openai

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.RealtimeUsage) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return types.NewError(fmt.Errorf("invalid websocket connection"), types.ErrorCodeBadResponse), nil
	}

	info.IsStream = true
	clientConn := info.ClientWs
	targetConn := info.TargetWs

	clientClosed := make(chan struct{})
	targetClosed := make(chan struct{})
	clientReaderDone := make(chan struct{})
	targetReaderDone := make(chan struct{})
	sendChan := make(chan []byte, 100)
	receiveChan := make(chan []byte, 100)
	clientErrChan := make(chan error, 1)
	targetErrChan := make(chan error, 1)
	policyErrChan := make(chan *types.NewAPIError, 1)
	var clientGone atomic.Bool
	var closeClientOnce sync.Once
	markClientGone := func() {
		clientGone.Store(true)
		closeClientOnce.Do(func() { close(clientClosed) })
	}

	usage := &dto.RealtimeUsage{}
	localUsage := &dto.RealtimeUsage{}
	sumUsage := &dto.RealtimeUsage{}
	var localUsageMu sync.Mutex

	gopool.Go(func() {
		defer close(clientReaderDone)
		defer markClientGone()
		defer func() {
			if r := recover(); r != nil {
				sendRealtimeRelayError(clientErrChan, fmt.Errorf("panic in client reader: %v", r))
			}
		}()
		for {
			select {
			case <-c.Done():
				return
			default:
				_, message, err := clientConn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						sendRealtimeRelayError(clientErrChan, fmt.Errorf("error reading from client: %v", err))
					}
					return
				}

				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					sendRealtimeRelayError(clientErrChan, fmt.Errorf("error unmarshalling message: %v", err))
					return
				}
				if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdate {
					if realtimeEvent.Session != nil {
						if realtimeEvent.Session.Tools != nil {
							info.RealtimeTools = realtimeEvent.Session.Tools
						}
					}
				}

				textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
				if err != nil {
					sendRealtimeRelayError(clientErrChan, fmt.Errorf("error counting text token: %v", err))
					return
				}
				logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
				localUsageMu.Lock()
				localUsage.TotalTokens += textToken + audioToken
				localUsage.InputTokens += textToken + audioToken
				localUsage.InputTokenDetails.TextTokens += textToken
				localUsage.InputTokenDetails.AudioTokens += audioToken
				localUsageMu.Unlock()

				err = helper.WssString(c, targetConn, string(message))
				if err != nil {
					sendRealtimeRelayError(clientErrChan, fmt.Errorf("error writing to target: %v", err))
					return
				}

				select {
				case sendChan <- message:
				default:
				}
			}
		}
	})

	gopool.Go(func() {
		defer close(targetReaderDone)
		defer func() {
			if r := recover(); r != nil {
				sendRealtimeRelayError(targetErrChan, fmt.Errorf("panic in target reader: %v", r))
			}
		}()
		requestDone := c.Done()
		for {
			select {
			case <-requestDone:
				// The request context can be canceled as soon as the downstream
				// disconnects. Keep reading the already-dispatched upstream turn
				// during the bounded policy drain so a terminal Cyber event is not
				// lost merely because the client closed first.
				markClientGone()
				requestDone = nil
			default:
				_, message, err := targetConn.ReadMessage()
				if err != nil {
					if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						sendRealtimeRelayError(targetErrChan, fmt.Errorf("error reading from target: %v", err))
					}
					close(targetClosed)
					return
				}
				info.SetFirstResponseTime()
				realtimeEvent := &dto.RealtimeEvent{}
				err = common.Unmarshal(message, realtimeEvent)
				if err != nil {
					sendRealtimeRelayError(targetErrChan, fmt.Errorf("error unmarshalling message: %v", err))
					return
				}
				if policyErr := realtimePolicyError(realtimeEvent); policyErr != nil {
					policyErrChan <- policyErr
					return
				}

				if realtimeEvent.Type == dto.RealtimeEventTypeResponseDone {
					realtimeUsage := realtimeEvent.Response.Usage
					if realtimeUsage != nil {
						usage.TotalTokens += realtimeUsage.TotalTokens
						usage.InputTokens += realtimeUsage.InputTokens
						usage.OutputTokens += realtimeUsage.OutputTokens
						usage.InputTokenDetails.AudioTokens += realtimeUsage.InputTokenDetails.AudioTokens
						usage.InputTokenDetails.CachedTokens += realtimeUsage.InputTokenDetails.CachedTokens
						usage.InputTokenDetails.TextTokens += realtimeUsage.InputTokenDetails.TextTokens
						usage.OutputTokenDetails.AudioTokens += realtimeUsage.OutputTokenDetails.AudioTokens
						usage.OutputTokenDetails.TextTokens += realtimeUsage.OutputTokenDetails.TextTokens
						err := preConsumeUsage(c, info, usage, sumUsage)
						if err != nil {
							sendRealtimeRelayError(targetErrChan, fmt.Errorf("error consume usage: %v", err))
							return
						}
						// 本次计费完成，清除
						usage = &dto.RealtimeUsage{}

						localUsageMu.Lock()
						localUsage = &dto.RealtimeUsage{}
						localUsageMu.Unlock()
					} else {
						textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
						if err != nil {
							sendRealtimeRelayError(targetErrChan, fmt.Errorf("error counting text token: %v", err))
							return
						}
						logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
						localUsageMu.Lock()
						localUsage.TotalTokens += textToken + audioToken
						info.IsFirstRequest = false
						localUsage.InputTokens += textToken + audioToken
						localUsage.InputTokenDetails.TextTokens += textToken
						localUsage.InputTokenDetails.AudioTokens += audioToken
						err = preConsumeUsage(c, info, localUsage, sumUsage)
						localUsage = &dto.RealtimeUsage{}
						localUsageMu.Unlock()
						if err != nil {
							sendRealtimeRelayError(targetErrChan, fmt.Errorf("error consume usage: %v", err))
							return
						}
					}
					logger.LogInfo(c, fmt.Sprintf("realtime streaming sumUsage: %v", sumUsage))
					localUsageMu.Lock()
					localUsageSnapshot := *localUsage
					localUsageMu.Unlock()
					logger.LogInfo(c, fmt.Sprintf("realtime streaming localUsage: %v", &localUsageSnapshot))

				} else if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdated || realtimeEvent.Type == dto.RealtimeEventTypeSessionCreated {
					realtimeSession := realtimeEvent.Session
					if realtimeSession != nil {
						// update audio format
						info.InputAudioFormat = common.GetStringIfEmpty(realtimeSession.InputAudioFormat, info.InputAudioFormat)
						info.OutputAudioFormat = common.GetStringIfEmpty(realtimeSession.OutputAudioFormat, info.OutputAudioFormat)
					}
				} else {
					textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
					if err != nil {
						sendRealtimeRelayError(targetErrChan, fmt.Errorf("error counting text token: %v", err))
						return
					}
					logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
					localUsageMu.Lock()
					localUsage.TotalTokens += textToken + audioToken
					localUsage.OutputTokens += textToken + audioToken
					localUsage.OutputTokenDetails.TextTokens += textToken
					localUsage.OutputTokenDetails.AudioTokens += audioToken
					localUsageMu.Unlock()
				}

				if !clientGone.Load() {
					err = helper.WssString(c, clientConn, string(message))
					if err != nil {
						markClientGone()
					}
				}

				select {
				case receiveChan <- message:
				default:
				}
			}
		}
	})

	var policyErr *types.NewAPIError
	clientEnded := false
	select {
	case <-clientClosed:
		clientEnded = true
	case <-targetClosed:
	case policyErr = <-policyErrChan:
	case err := <-clientErrChan:
		clientEnded = true
		logger.LogError(c, "realtime client error: "+err.Error())
	case err := <-targetErrChan:
		logger.LogError(c, "realtime error: "+err.Error())
	case <-c.Done():
		markClientGone()
		clientEnded = true
	}
	if clientEnded && policyErr == nil {
		timer := time.NewTimer(realtimePolicyDrainTimeout)
		select {
		case policyErr = <-policyErrChan:
		case <-targetClosed:
		case err := <-targetErrChan:
			logger.LogError(c, "realtime error while draining upstream policy: "+err.Error())
		case <-timer.C:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
	if policyErr == nil {
		select {
		case policyErr = <-policyErrChan:
		default:
		}
	}
	stopRealtimeReader(clientConn, clientReaderDone)
	stopRealtimeReader(targetConn, targetReaderDone)

	if usage.TotalTokens != 0 {
		_ = preConsumeUsage(c, info, usage, sumUsage)
	}

	localUsageMu.Lock()
	if localUsage.TotalTokens != 0 {
		_ = preConsumeUsage(c, info, localUsage, sumUsage)
	}
	localUsageMu.Unlock()

	// check usage total tokens, if 0, use local usage

	return policyErr, sumUsage
}

func preConsumeUsage(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.RealtimeUsage, totalUsage *dto.RealtimeUsage) error {
	if usage == nil || totalUsage == nil {
		return fmt.Errorf("invalid usage pointer")
	}

	totalUsage.TotalTokens += usage.TotalTokens
	totalUsage.InputTokens += usage.InputTokens
	totalUsage.OutputTokens += usage.OutputTokens
	totalUsage.InputTokenDetails.CachedTokens += usage.InputTokenDetails.CachedTokens
	totalUsage.InputTokenDetails.TextTokens += usage.InputTokenDetails.TextTokens
	totalUsage.InputTokenDetails.AudioTokens += usage.InputTokenDetails.AudioTokens
	totalUsage.OutputTokenDetails.TextTokens += usage.OutputTokenDetails.TextTokens
	totalUsage.OutputTokenDetails.AudioTokens += usage.OutputTokenDetails.AudioTokens
	// clear usage
	err := service.PreWssConsumeQuota(ctx, info, usage)
	return err
}
