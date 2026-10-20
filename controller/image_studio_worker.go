package controller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"github.com/gin-gonic/gin"
)

func RegisterImageGenerationBackgroundJobs(registry *service.BackgroundJobRegistry, workerID string) error {
	processor := service.NewKKAIOutboxProcessor(model.DB, workerID+"-image-generation")
	if err := processor.Register(service.ImageGenerateTopic, runImageGenerationEvent); err != nil {
		return err
	}
	return registry.Register(service.BackgroundJob{
		Name: "image-studio-generation", Interval: 2 * time.Second, RunOnStart: true, WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			return processor.RunConcurrent(ctx, 2*time.Second, func(ctx context.Context) (context.Context, func(), error) {
				if !image_studio_setting.Get().WorkerEnabled || !service.ImageAsyncSchemaReady(ctx, model.DB) {
					return ctx, nil, nil
				}
				finish, ok := service.BeginImageWorkerOperation()
				if !ok {
					return ctx, nil, nil
				}
				ctx, err := service.WithCurrentImageWorkerLease(ctx, workerID)
				if err != nil {
					finish()
					return ctx, nil, err
				}
				return ctx, finish, nil
			})
		},
	})
}

func runImageGenerationEvent(ctx context.Context, event model.KKAIOutboxEvent) (handlerErr error) {
	id, err := strconv.ParseInt(event.AggregateID, 10, 64)
	if err != nil || id <= 0 {
		return service.PermanentKKAIOutboxError(service.ErrInvalidImageStudioSubmission)
	}
	// An accepted generation must remain recoverable until settlement/refund
	// commits. Infrastructure failures must not exhaust the generic outbox budget.
	defer func() {
		if recover() != nil {
			handlerErr = errors.New("image execution interrupted unexpectedly")
		}
		if handlerErr != nil {
			// The generic outbox persists error text. Do not persist provider bodies,
			// object URLs or other raw transport errors in its retry audit.
			handlerErr = service.DeferKKAIOutboxUntil(time.Now().Add(15*time.Second), errors.New("image_execution_recovery_pending"))
		}
	}()
	fence, err := service.ImageWorkerFence(ctx, event)
	if err != nil {
		return err
	}
	execution, generation, err := service.AdoptImageExecution(ctx, model.DB, id, fence)
	if err != nil {
		return err
	}
	if execution.State == model.ImageGenerationExecutionDone {
		return nil
	}
	ctx = service.WithImageExecutionFence(ctx, id, fence)
	if execution.State == model.ImageGenerationExecutionResultReady || execution.State == model.ImageGenerationExecutionDispatching || execution.State == model.ImageGenerationExecutionUnknown {
		// An interrupted request is never automatically submitted again, even if the
		// provider has not supplied a request ID or an API for retrieving its result.
		if err := service.CompleteImageExecution(ctx, model.DB, id, fence, ""); err == nil {
			return nil
		} else if !errors.Is(err, model.ErrImageAccountingNotReady) {
			return err
		}
		if execution.RecoveryDeadlineAt <= time.Now().Unix() {
			return service.CompleteImageExecution(ctx, model.DB, id, fence, "upstream_outcome_unknown")
		}
		return service.DeferKKAIOutboxUntil(time.Now().Add(15*time.Second), model.ErrImageAccountingNotReady)
	}
	if execution.ExecutionDeadlineAt == 0 || execution.ExecutionDeadlineAt <= time.Now().Unix() {
		return service.CompleteImageExecution(ctx, model.DB, id, fence, "image_queue_expired")
	}
	var payload service.ImageExecutionPayload
	if common.UnmarshalJsonStr(execution.Payload, &payload) != nil || payload.Version != 1 || payload.Submission.RelayRequest == nil || payload.Quote.ImagePricingSnapshot == nil {
		return service.CompleteImageExecution(ctx, model.DB, id, fence, "invalid_execution_payload")
	}
	runCtx, cancel := context.WithDeadline(ctx, time.Unix(execution.ExecutionDeadlineAt, 0))
	defer cancel()
	err = executeAsyncImage(runCtx, ctx, generation, execution, &payload, fence)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Inspect the durable marker, never infer whether a POST escaped from an error
	// string. A marker written before I/O deliberately errs on the side of unknown.
	var stored model.KKAIImageExecution
	if readErr := model.DB.WithContext(ctx).First(&stored, "generation_id = ?", id).Error; readErr != nil {
		return readErr
	}
	switch stored.State {
	case model.ImageGenerationExecutionDone:
		return nil
	case model.ImageGenerationExecutionQueued, model.ImageGenerationExecutionPreparing:
		return service.CompleteImageExecution(ctx, model.DB, id, fence, "image_prepare_failed")
	case model.ImageGenerationExecutionResultReady:
		return service.CompleteImageExecution(ctx, model.DB, id, fence, "")
	default:
		if err := service.MarkImageExecutionUnknown(ctx, model.DB, id, fence, stored.UpstreamRequestID); err != nil {
			return err
		}
		logger.LogWarn(ctx, "event=image_async_unknown generation_id=%d request_id=%q channel_id=%d code=upstream_outcome_pending", generation.ID, generation.RequestID, stored.DispatchChannelID)
		return service.DeferKKAIOutboxUntil(time.Now().Add(15*time.Second), errors.New("image provider outcome pending reconciliation"))
	}
}

type imageWorkerResponse struct{ header http.Header }

func (w *imageWorkerResponse) Header() http.Header         { return w.header }
func (w *imageWorkerResponse) Write(p []byte) (int, error) { return len(p), nil }
func (w *imageWorkerResponse) WriteHeader(int)             {}

func executeAsyncImage(runCtx, workerCtx context.Context, generation *model.KKAIImageGeneration, execution *model.KKAIImageExecution, payload *service.ImageExecutionPayload, fence model.ImageExecutionFence) error {
	settings := image_studio_setting.Get()
	// Match an incoming server request: adapters append this path to their base URL.
	request, err := http.NewRequestWithContext(service.WithImageStudioResponseLimit(runCtx, settings.MaxResponseBytes), http.MethodPost, "/v1/images/generations", http.NoBody)
	if err != nil {
		return err
	}
	request.RemoteAddr = net.JoinHostPort(payload.ClientIP, "0")
	// Gin's constructor initializes its response wrapper; no HTTP listener is used.
	c, _ := gin.CreateTestContext(&imageWorkerResponse{header: make(http.Header)})
	c.Request = request
	c.Set("id", generation.UserID)
	c.Set(common.RequestIdKey, generation.RequestID)
	if err := prepareAsyncImageContext(c, &payload.Submission); err != nil {
		return err
	}
	if err := service.SetImageStudioGenerationID(c, generation.ID); err != nil {
		return err
	}
	if err := service.SetImageStudioBillingGuard(c, payload.Quote.Quota); err != nil {
		return err
	}
	if err := service.SetTrustedImagePricingSnapshot(c, *payload.Quote.ImagePricingSnapshot); err != nil {
		return err
	}
	store, err := service.ImageStudioR2AssetStore(runCtx)
	if err != nil {
		return err
	}
	archives := make([]*service.FetchedImageArchive, 0, len(payload.AssetIDs))
	defer func() {
		for _, archive := range archives {
			archive.Remove()
		}
	}()
	defer common.CleanupBodyStorage(c)
	if payload.Submission.Mode == service.ImageStudioModeEdit {
		fetcher := service.NewHTTPImageArchiveFetcher(service.ImageStudioTempDirectory())
		for index, id := range payload.AssetIDs {
			var asset model.KKAIImageAsset
			if err := model.DB.WithContext(runCtx).Where("id = ? AND owner_user_id = ? AND kind = ? AND state = ? AND deleted_at = 0", id, generation.UserID, model.ImageAssetKindReference, model.ImageAssetStateReady).Take(&asset).Error; err != nil {
				return err
			}
			object, err := store.Get(runCtx, asset.ObjectKey)
			if err != nil {
				return err
			}
			archive, ingestErr := fetcher.Ingest(object.Body, asset.MIMEType, settings.MaxReferenceBytes, settings.MaxPixels)
			closeErr := object.Body.Close()
			if archive != nil {
				archives = append(archives, archive)
			}
			if ingestErr != nil || closeErr != nil {
				return errors.Join(ingestErr, closeErr)
			}
			if index >= len(payload.Submission.References) || archive.SHA256 != payload.Submission.References[index].SHA256 || archive.SizeBytes != payload.Submission.References[index].SizeBytes {
				return service.ErrImageUploadConflict
			}
		}
		if err := replaceImageStudioEditRelayBody(c, payload.Submission.RelayRequest, archives); err != nil {
			return err
		}
	} else if err := replaceImageStudioRelayBody(c, payload.Submission.RelayRequest); err != nil {
		return err
	}
	if err := selectAsyncImageChannel(c, payload.Submission.Model); err != nil {
		return err
	}
	relayRequest, err := helper.GetAndValidateRequest(c, relaytypes.RelayFormatOpenAIImage)
	if err != nil {
		return err
	}
	info, err := relaycommon.GenRelayInfo(c, relaytypes.RelayFormatOpenAIImage, relayRequest, nil)
	if err != nil {
		return err
	}
	info.DurableImageExecution = true
	if err := service.PrepareImagePricing(c, info); err != nil {
		return err
	}
	meta := relayRequest.GetTokenCountMeta()
	tokens, err := service.EstimateRequestToken(c, meta, info)
	if err != nil {
		return err
	}
	info.SetEstimatePromptTokens(tokens)
	if _, err := helper.ModelPriceHelper(c, info, tokens, meta); err != nil {
		return err
	}
	if err := service.RestoreImageGenerationBilling(c, model.DB, generation, info, fence); err != nil {
		return err
	}
	capture, err := newImageRelayCaptureWriter(c.Writer, service.ImageStudioTempDirectory(), settings.MaxResponseBytes)
	if err != nil {
		return err
	}
	defer capture.Remove()
	c.Writer = capture
	upstreamStarted := time.Now()
	relayErr := relay.ImageHelper(c, info)
	closeErr := capture.Close()
	upstreamCode := "ok"
	if relayErr != nil || capture.err != nil || closeErr != nil || capture.Status() < 200 || capture.Status() >= 300 {
		upstreamCode = "upstream_failed"
	}
	logger.LogInfo(c, fmt.Sprintf("event=image_async_upstream_finished generation_id=%d request_id=%q elapsed_ms=%d channel_id=%d code=%s", generation.ID, generation.RequestID, time.Since(upstreamStarted).Milliseconds(), c.GetInt("channel_id"), upstreamCode))
	if relayErr != nil {
		return relayErr
	}
	if capture.err != nil || closeErr != nil {
		return errors.Join(capture.err, closeErr)
	}
	if capture.Status() < 200 || capture.Status() >= 300 {
		return errors.New("image provider returned an unsuccessful response")
	}
	_, prepared, capped := service.ImageStudioFinalQuota(c)
	if !prepared || capped {
		return service.ErrImageStudioQuoteStale
	}
	results, err := service.ParseImageRelayResponseFile(capture.Path(), settings.MaxResponseBytes, payload.Submission.RequestedCount)
	if err != nil {
		return err
	}
	archiveCtx, archiveCancel := context.WithTimeout(workerCtx, service.ImageArchiveTimeout)
	defer archiveCancel()
	c.Request = c.Request.WithContext(archiveCtx)
	pipeline, err := imageStudioAssetPipeline(c)
	if err != nil {
		return err
	}
	archiveStarted := time.Now()
	archive, archiveErr := pipeline.ArchiveGeneration(archiveCtx, *generation, results)
	readyCount, archiveCode := 0, "ok"
	if archive != nil {
		readyCount = archive.Ready
	}
	if archiveErr != nil || readyCount == 0 {
		archiveCode = "archive_incomplete"
	}
	logger.LogInfo(c, fmt.Sprintf("event=image_async_archive_finished generation_id=%d request_id=%q elapsed_ms=%d output_count=%d code=%s", generation.ID, generation.RequestID, time.Since(archiveStarted).Milliseconds(), readyCount, archiveCode))
	if archive == nil || archive.Ready == 0 {
		return errors.Join(archiveErr, errors.New("no image result was archived"))
	}
	if archiveErr != nil || archive.Failed > 0 {
		if err := service.DiscardNonReadySubmittingImageGenerationAssets(archiveCtx, model.DB, generation.ID); err != nil {
			return err
		}
	}
	if err := service.PrepareImageStudioDeliveryBilling(c, len(results), archive.Ready); err != nil {
		return err
	}
	return service.CompleteImageExecution(archiveCtx, model.DB, generation.ID, fence, "")
}
