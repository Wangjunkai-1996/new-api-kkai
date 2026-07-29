package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"gorm.io/gorm"
)

type VideoAssetPipeline struct {
	db                  *gorm.DB
	store               VideoAssetStore
	media               VideoMediaProcessor
	fetcher             VideoArchiveSourceFetcher
	inspectTopic        string
	posterTopic         string
	tempDir             string
	now                 func() time.Time
	archiveFetchTimeout time.Duration
	archivePutTimeout   time.Duration
}

const (
	videoSamplePreparationReadinessTimeout = 10 * time.Minute
	videoArchiveFetchHardTimeout           = 15 * time.Minute
	videoArchivePutHardTimeout             = 15 * time.Minute
)

var errVideoArchiveTaskSourceChanged = errors.New("video archive task source changed")

type resolvedVideoArchiveSource struct {
	assetSource        string
	taskID             int64
	expectedTaskSource string
	fetch              videoArchiveTaskSource
}

func NewVideoAssetPipeline(db *gorm.DB, store VideoAssetStore, media VideoMediaProcessor, fetcher VideoArchiveSourceFetcher, tempDir string) (*VideoAssetPipeline, error) {
	tempDir = strings.TrimSpace(tempDir)
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	if db == nil || store == nil || media == nil || fetcher == nil {
		return nil, ErrInvalidVideoOutboxEvent
	}
	info, err := os.Stat(tempDir)
	if err != nil || !info.IsDir() {
		return nil, ErrVideoTemporaryStorageUnavailable
	}
	return &VideoAssetPipeline{
		db: db, store: store, media: media, fetcher: fetcher,
		inspectTopic: VideoOutboxTopicInspect, posterTopic: VideoOutboxTopicPoster,
		tempDir: tempDir, now: time.Now,
		archiveFetchTimeout: videoArchiveFetchHardTimeout,
		archivePutTimeout:   videoArchivePutHardTimeout,
	}, nil
}

func (pipeline *VideoAssetPipeline) Register(processor *KKAIOutboxProcessor) error {
	if pipeline == nil || processor == nil {
		return ErrInvalidVideoOutboxEvent
	}
	handlers := map[string]KKAIOutboxHandler{
		VideoOutboxTopicInspect:       pipeline.HandleInspect,
		VideoOutboxTopicArchive:       pipeline.HandleArchive,
		VideoOutboxTopicPoster:        pipeline.HandlePoster,
		VideoOutboxTopicDelete:        pipeline.HandleDelete,
		VideoOutboxTopicSamplePrepare: pipeline.HandleSamplePrepare,
	}
	for topic, handler := range handlers {
		if err := processor.Register(topic, handler); err != nil {
			return err
		}
		if err := processor.registerDeadLetter(topic, pipeline.handleDeadLetter); err != nil {
			return err
		}
	}
	return nil
}

func (pipeline *VideoAssetPipeline) handleDeadLetter(
	ctx context.Context,
	tx *gorm.DB,
	event model.KKAIOutboxEvent,
	deliveryErr error,
	now time.Time,
) error {
	assetID, _, err := videoOutboxAssetRetryState(ctx, tx, event)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrInvalidVideoOutboxEvent) {
			return nil
		}
		return err
	}
	query := tx.WithContext(ctx).Model(&model.KKAIVideoAsset{})
	if event.Topic == VideoOutboxTopicDelete {
		query = query.Where("id = ? AND state = ?", assetID, model.VideoAssetStateDeleting)
	} else {
		query = query.Where("id = ? AND state NOT IN ?", assetID, []string{
			model.VideoAssetStateFailed,
			model.VideoAssetStateDeleting,
			model.VideoAssetStateDeleted,
		})
	}
	return query.Updates(map[string]any{
		"state": model.VideoAssetStateFailed, "failure_reason": videoAssetFailureReason(deliveryErr),
		"updated_at": now.Unix(),
	}).Error
}

func (pipeline *VideoAssetPipeline) HandleArchive(ctx context.Context, event model.KKAIOutboxEvent) error {
	asset, err := pipeline.videoAssetFromEvent(ctx, event)
	if err != nil || asset == nil {
		return err
	}
	if asset.State == model.VideoAssetStateDeleting || asset.State == model.VideoAssetStateDeleted {
		return pipeline.store.Delete(ctx, []string{asset.ObjectKey})
	}
	if asset.State == model.VideoAssetStateFailed {
		return nil
	}
	if asset.ArchiveSourceURL == "" {
		return pipeline.enqueueAssetEvent(ctx, asset.ID, pipeline.inspectTopic, "inspect:v1")
	}
	archiveSource, err := pipeline.resolveVideoArchiveSource(ctx, *asset)
	if err != nil {
		return err
	}
	archiveSourceSHA256 := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(archiveSource.fetch.Source))))
	settings := video_studio_setting.Get()
	metadata, err := pipeline.store.Head(ctx, asset.ObjectKey)
	if err == nil {
		if validArchivedVideoObject(metadata, settings.MaxArchivedVideoBytes) &&
			metadata.ArchiveSourceSHA256 == archiveSourceSHA256 {
			return pipeline.completeArchivedVideoObject(ctx, *asset, archiveSource, metadata)
		}
		if err := pipeline.store.Delete(ctx, []string{asset.ObjectKey}); err != nil {
			return err
		}
	} else if !errors.Is(err, ErrVideoAssetObjectNotFound) {
		return err
	}
	fetchTimeout := pipeline.archiveFetchTimeout
	if fetchTimeout <= 0 {
		fetchTimeout = videoArchiveFetchHardTimeout
	}
	fetchCtx, cancelFetch := context.WithTimeout(ctx, fetchTimeout)
	var fetched *FetchedVideoArchive
	if archiveSource.taskID == 0 {
		fetched, err = pipeline.fetcher.Fetch(fetchCtx, archiveSource.fetch.Source, settings.MaxArchivedVideoBytes)
	} else {
		taskFetcher, ok := pipeline.fetcher.(videoArchiveTaskSourceFetcher)
		if !ok {
			cancelFetch()
			return ErrVideoArchiveSourceRejected
		}
		fetched, err = taskFetcher.FetchTaskSource(fetchCtx, archiveSource.fetch, settings.MaxArchivedVideoBytes)
	}
	cancelFetch()
	if err != nil {
		return pipeline.assetProcessingError(ctx, asset.ID, err)
	}
	defer fetched.Remove()
	file, err := os.Open(fetched.Path)
	if err != nil {
		return err
	}
	putTimeout := pipeline.archivePutTimeout
	if putTimeout <= 0 {
		putTimeout = videoArchivePutHardTimeout
	}
	putCtx, cancelPut := context.WithTimeout(ctx, putTimeout)
	var putErr error
	if archiveStore, ok := pipeline.store.(VideoArchiveAssetStore); ok {
		putErr = archiveStore.PutArchive(
			putCtx, asset.ObjectKey, fetched.MIMEType, file, fetched.SizeBytes, fetched.SHA256, archiveSourceSHA256,
		)
	} else {
		putErr = pipeline.store.Put(putCtx, asset.ObjectKey, fetched.MIMEType, file, fetched.SizeBytes)
	}
	cancelPut()
	closeErr := file.Close()
	if putErr != nil {
		return putErr
	}
	if closeErr != nil {
		return closeErr
	}
	return pipeline.completeArchivedVideoObject(ctx, *asset, archiveSource, VideoAssetObjectMetadata{
		ContentType: fetched.MIMEType, ContentLength: fetched.SizeBytes, SHA256: fetched.SHA256,
		ArchiveSourceSHA256: archiveSourceSHA256,
	})
}

func (pipeline *VideoAssetPipeline) completeArchivedVideoObject(
	ctx context.Context,
	asset model.KKAIVideoAsset,
	source resolvedVideoArchiveSource,
	metadata VideoAssetObjectMetadata,
) error {
	err := pipeline.completeVideoAssetArchive(ctx, asset.ID, source, metadata)
	if !errors.Is(err, errVideoArchiveTaskSourceChanged) {
		return err
	}
	if deleteErr := pipeline.store.Delete(ctx, []string{asset.ObjectKey}); deleteErr != nil {
		return deleteErr
	}
	return err
}

func (pipeline *VideoAssetPipeline) completeVideoAssetArchive(
	ctx context.Context,
	assetID int64,
	source resolvedVideoArchiveSource,
	metadata VideoAssetObjectMetadata,
) error {
	now := pipeline.now().Unix()
	discardObject := false
	objectKey := ""
	err := pipeline.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		update := tx.Model(&model.KKAIVideoAsset{}).
			Where("id = ? AND archive_source_url = ? AND state NOT IN ?", assetID, source.assetSource,
				[]string{model.VideoAssetStateDeleting, model.VideoAssetStateDeleted}).
			Updates(map[string]any{
				"archive_source_url": "", "mime_type": normalizedVideoObjectContentType(metadata.ContentType), "size_bytes": metadata.ContentLength,
				"sha256": strings.ToLower(metadata.SHA256), "state": model.VideoAssetStateProcessing, "failure_reason": "", "updated_at": now,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			var current model.KKAIVideoAsset
			if err := tx.Select("state, object_key").First(&current, "id = ?", assetID).Error; err != nil {
				return err
			}
			discardObject = current.State == model.VideoAssetStateDeleting || current.State == model.VideoAssetStateDeleted
			objectKey = current.ObjectKey
			return nil
		}
		if source.taskID > 0 {
			cleared, err := model.ClearAssetHostedTaskResultSource(ctx, tx, source.taskID, source.expectedTaskSource)
			if err != nil {
				return err
			}
			if !cleared {
				return errVideoArchiveTaskSourceChanged
			}
		}
		return EnqueueVideoOutboxEvent(ctx, tx,
			fmt.Sprintf("video:asset:%d:inspect:v1", assetID), pipeline.inspectTopic,
			strconv.FormatInt(assetID, 10), VideoAssetEventPayload{AssetID: assetID},
		)
	})
	if err != nil || !discardObject || objectKey == "" {
		return err
	}
	return pipeline.store.Delete(ctx, []string{objectKey})
}

func (pipeline *VideoAssetPipeline) resolveVideoArchiveSource(ctx context.Context, asset model.KKAIVideoAsset) (resolvedVideoArchiveSource, error) {
	resolved := resolvedVideoArchiveSource{
		assetSource: asset.ArchiveSourceURL,
		fetch:       videoArchiveTaskSource{Source: asset.ArchiveSourceURL},
	}
	taskID, managed := parseVideoTaskResultArchiveSource(asset.ArchiveSourceURL)
	if !managed {
		return resolved, nil
	}
	var link model.KKAIVideoTaskAsset
	if err := pipeline.db.WithContext(ctx).First(&link,
		"task_id = ? AND asset_id = ? AND role = ? AND position = ?",
		taskID, asset.ID, model.VideoTaskAssetRoleOutput, 0,
	).Error; err != nil {
		return resolvedVideoArchiveSource{}, ErrVideoArchiveSourceRejected
	}
	var task model.Task
	if err := pipeline.db.WithContext(ctx).First(&task, "id = ?", taskID).Error; err != nil || !task.IsAssetHostedResult() {
		return resolvedVideoArchiveSource{}, ErrVideoArchiveSourceRejected
	}

	resolved.taskID = task.ID
	resolved.expectedTaskSource = task.PrivateData.ArchiveSource
	resolved.fetch.Source = strings.TrimSpace(task.PrivateData.ArchiveSource)
	if strings.HasPrefix(resolved.fetch.Source, "data:") {
		return resolved, nil
	}
	providerContent := resolved.fetch.Source == "" || strings.Contains(
		resolved.fetch.Source,
		"/v1/videos/"+task.TaskID+"/content",
	)

	var channel model.Channel
	if err := pipeline.db.WithContext(ctx).First(&channel, "id = ?", task.ChannelId).Error; err != nil {
		return resolvedVideoArchiveSource{}, err
	}
	resolved.fetch.ProxyURL = channel.GetSetting().Proxy
	if providerContent {
		switch channel.Type {
		case constant.ChannelTypeOpenAI, constant.ChannelTypeSora:
			baseURL := strings.TrimRight(channel.GetBaseURL(), "/")
			if baseURL == "" {
				baseURL = strings.TrimRight(constant.ChannelBaseURLs[channel.Type], "/")
			}
			if baseURL == "" || task.GetUpstreamTaskID() == "" {
				return resolvedVideoArchiveSource{}, ErrVideoArchiveSourceRejected
			}
			resolved.fetch.Source = fmt.Sprintf("%s/v1/videos/%s/content", baseURL, url.PathEscape(task.GetUpstreamTaskID()))
			resolved.fetch.Headers = map[string]string{"Authorization": "Bearer " + channel.Key}
			resolved.fetch.ProviderContentBaseURL = baseURL
		default:
			refreshed, err := pipeline.refreshVideoArchiveSource(ctx, task, channel)
			if err != nil {
				return resolvedVideoArchiveSource{}, err
			}
			resolved.fetch.Source = refreshed
		}
	}
	if channel.Type == constant.ChannelTypeGemini {
		geminiBaseURL := channel.GetBaseURL()
		if geminiBaseURL == "" {
			geminiBaseURL = constant.ChannelBaseURLs[channel.Type]
		}
		if VideoSourceCanUseProviderCredentials(resolved.fetch.Source, geminiBaseURL) {
			key := strings.TrimSpace(task.PrivateData.Key)
			if key == "" {
				key = strings.TrimSpace(channel.Key)
			}
			if key == "" {
				return resolvedVideoArchiveSource{}, ErrVideoArchiveSourceRejected
			}
			resolved.fetch.Headers = map[string]string{"x-goog-api-key": key}
		}
	}
	return resolved, nil
}

func (pipeline *VideoAssetPipeline) refreshVideoArchiveSource(ctx context.Context, task model.Task, channel model.Channel) (string, error) {
	if GetTaskAdaptorFunc == nil {
		return "", ErrVideoArchiveSourceRejected
	}
	adaptor := GetTaskAdaptorFunc(task.Platform)
	if adaptor == nil {
		return "", ErrVideoArchiveSourceRejected
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[channel.Type]
	}
	key := channel.Key
	if task.PrivateData.Key != "" {
		key = task.PrivateData.Key
	}
	adaptor.Init(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: baseURL, ApiKey: key},
	})
	response, err := adaptor.FetchTask(baseURL, key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, channel.GetSetting().Proxy)
	if err != nil {
		return "", err
	}
	if response == nil || response.Body == nil {
		return "", ErrVideoArchiveResponseRejected
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", ErrVideoArchiveResponseRejected
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	result, err := adaptor.ParseTaskResult(body)
	if err != nil || result == nil {
		return "", ErrVideoArchiveResponseRejected
	}
	if source := strings.TrimSpace(result.Url); source != "" {
		return source, nil
	}
	if source := strings.TrimSpace(result.RemoteUrl); source != "" {
		return source, nil
	}
	return "", ErrVideoArchiveResponseRejected
}

func validArchivedVideoObject(metadata VideoAssetObjectMetadata, maxBytes int64) bool {
	return metadata.ContentLength > 0 && metadata.ContentLength <= maxBytes &&
		isSupportedVideoMIME(normalizedVideoObjectContentType(metadata.ContentType)) && validSHA256Hex(strings.ToLower(metadata.SHA256))
}

func normalizedVideoObjectContentType(contentType string) string {
	return strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
}

func (pipeline *VideoAssetPipeline) HandleInspect(ctx context.Context, event model.KKAIOutboxEvent) error {
	asset, err := pipeline.videoAssetFromEvent(ctx, event)
	if err != nil || asset == nil {
		return err
	}
	if asset.State == model.VideoAssetStateReady || asset.State == model.VideoAssetStateDeleting ||
		asset.State == model.VideoAssetStateDeleted || asset.State == model.VideoAssetStateFailed {
		return nil
	}
	path, remove, err := pipeline.copyObjectToTemporaryFile(ctx, *asset)
	if err != nil {
		return pipeline.assetProcessingError(ctx, asset.ID, err)
	}
	defer remove()
	metadata, err := pipeline.media.Inspect(ctx, path)
	if err != nil {
		return pipeline.assetProcessingError(ctx, asset.ID, err)
	}
	if asset.Kind == model.VideoAssetKindReference && !strings.HasPrefix(metadata.MIMEType, "image/") {
		return pipeline.assetProcessingError(ctx, asset.ID, ErrVideoMediaInvalid)
	}
	if asset.Kind != model.VideoAssetKindReference && !strings.HasPrefix(metadata.MIMEType, "video/") {
		return pipeline.assetProcessingError(ctx, asset.ID, ErrVideoMediaInvalid)
	}
	now := pipeline.now().Unix()
	updates := map[string]any{
		"mime_type": metadata.MIMEType, "width": metadata.Width, "height": metadata.Height,
		"duration_seconds": metadata.DurationSeconds, "codec": metadata.Codec,
		"failure_reason": "", "updated_at": now,
	}
	if strings.HasPrefix(metadata.MIMEType, "image/") {
		updates["state"] = model.VideoAssetStateReady
		return pipeline.db.WithContext(ctx).Model(&model.KKAIVideoAsset{}).
			Where("id = ? AND state NOT IN ?", asset.ID,
				[]string{model.VideoAssetStateDeleting, model.VideoAssetStateDeleted, model.VideoAssetStateFailed}).
			Updates(updates).Error
	}
	updates["state"] = model.VideoAssetStateProcessing
	return pipeline.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		update := tx.Model(&model.KKAIVideoAsset{}).
			Where("id = ? AND state NOT IN ?", asset.ID,
				[]string{model.VideoAssetStateDeleting, model.VideoAssetStateDeleted, model.VideoAssetStateFailed}).
			Updates(updates)
		if update.Error != nil || update.RowsAffected == 0 {
			return update.Error
		}
		return EnqueueVideoOutboxEvent(ctx, tx,
			fmt.Sprintf("video:asset:%d:poster:v1", asset.ID), pipeline.posterTopic,
			strconv.FormatInt(asset.ID, 10), VideoAssetEventPayload{AssetID: asset.ID},
		)
	})
}

func (pipeline *VideoAssetPipeline) HandlePoster(ctx context.Context, event model.KKAIOutboxEvent) error {
	asset, err := pipeline.videoAssetFromEvent(ctx, event)
	if err != nil || asset == nil {
		return err
	}
	posterKey := videoAssetDerivedPath(asset.ObjectKey, ".poster.jpg")
	if asset.State == model.VideoAssetStateDeleting || asset.State == model.VideoAssetStateDeleted {
		return pipeline.store.Delete(ctx, []string{posterKey})
	}
	if asset.State == model.VideoAssetStateFailed {
		return nil
	}
	if asset.PosterObjectKey != "" {
		return pipeline.markVideoAssetReady(ctx, asset.ID)
	}
	inputPath, removeInput, err := pipeline.copyObjectToTemporaryFile(ctx, *asset)
	if err != nil {
		return pipeline.assetProcessingError(ctx, asset.ID, err)
	}
	defer removeInput()
	output, err := os.CreateTemp(pipeline.tempDir, "new-api-video-poster-*.jpg")
	if err != nil {
		return ErrVideoTemporaryStorageUnavailable
	}
	outputPath := output.Name()
	_ = output.Close()
	defer os.Remove(outputPath)
	if err := pipeline.media.CreatePoster(ctx, inputPath, outputPath, videoPosterMaximumBytes); err != nil {
		return pipeline.assetProcessingError(ctx, asset.ID, err)
	}
	posterFile, err := os.Open(outputPath)
	if err != nil {
		return err
	}
	info, err := posterFile.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > videoPosterMaximumBytes {
		_ = posterFile.Close()
		return pipeline.assetProcessingError(ctx, asset.ID, ErrVideoMediaProcessingFailed)
	}
	putErr := pipeline.store.Put(ctx, posterKey, "image/jpeg", posterFile, info.Size())
	closeErr := posterFile.Close()
	if putErr != nil {
		return putErr
	}
	if closeErr != nil {
		return closeErr
	}
	update := pipeline.db.WithContext(ctx).Model(&model.KKAIVideoAsset{}).
		Where("id = ? AND state NOT IN ?", asset.ID,
			[]string{model.VideoAssetStateDeleting, model.VideoAssetStateDeleted, model.VideoAssetStateFailed}).
		Updates(map[string]any{
			"poster_object_key": posterKey, "state": model.VideoAssetStateReady,
			"failure_reason": "", "updated_at": pipeline.now().Unix(),
		})
	if update.Error != nil || update.RowsAffected == 1 {
		return update.Error
	}
	return pipeline.store.Delete(ctx, []string{posterKey})
}

func (pipeline *VideoAssetPipeline) prepareSamplePreview(ctx context.Context, asset *model.KKAIVideoAsset) error {
	if asset == nil {
		return ErrVideoMediaInvalid
	}
	previewKey := videoAssetDerivedPath(asset.ObjectKey, ".preview.mp4")
	if asset.State == model.VideoAssetStateDeleting || asset.State == model.VideoAssetStateDeleted {
		return pipeline.store.Delete(ctx, []string{previewKey})
	}
	if asset.State == model.VideoAssetStateFailed || asset.PreviewObjectKey != "" {
		return nil
	}
	if asset.Scope != model.VideoAssetScopeCatalog || asset.Kind != model.VideoAssetKindSample || asset.PosterObjectKey == "" {
		return pipeline.assetProcessingError(ctx, asset.ID, ErrVideoMediaInvalid)
	}
	inputPath, removeInput, err := pipeline.copyObjectToTemporaryFile(ctx, *asset)
	if err != nil {
		return pipeline.assetProcessingError(ctx, asset.ID, err)
	}
	defer removeInput()
	output, err := os.CreateTemp(pipeline.tempDir, "new-api-video-preview-*.mp4")
	if err != nil {
		return ErrVideoTemporaryStorageUnavailable
	}
	outputPath := output.Name()
	_ = output.Close()
	defer os.Remove(outputPath)
	if err := pipeline.media.CreatePreview(ctx, inputPath, outputPath); err != nil {
		return pipeline.assetProcessingError(ctx, asset.ID, err)
	}
	previewFile, err := os.Open(outputPath)
	if err != nil {
		return err
	}
	info, err := previewFile.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > videoPreviewMaximumBytes {
		_ = previewFile.Close()
		return pipeline.assetProcessingError(ctx, asset.ID, ErrVideoMediaProcessingFailed)
	}
	putErr := pipeline.store.Put(ctx, previewKey, "video/mp4", previewFile, info.Size())
	closeErr := previewFile.Close()
	if putErr != nil {
		return putErr
	}
	if closeErr != nil {
		return closeErr
	}
	update := pipeline.db.WithContext(ctx).Model(&model.KKAIVideoAsset{}).
		Where("id = ? AND state NOT IN ?", asset.ID,
			[]string{model.VideoAssetStateDeleting, model.VideoAssetStateDeleted, model.VideoAssetStateFailed}).
		Updates(map[string]any{
			"preview_object_key": previewKey, "state": model.VideoAssetStateReady,
			"failure_reason": "", "updated_at": pipeline.now().Unix(),
		})
	if update.Error != nil || update.RowsAffected == 1 {
		return update.Error
	}
	return pipeline.store.Delete(ctx, []string{previewKey})
}

func (pipeline *VideoAssetPipeline) HandleDelete(ctx context.Context, event model.KKAIOutboxEvent) error {
	asset, err := pipeline.videoAssetFromEvent(ctx, event)
	if err != nil || asset == nil {
		return err
	}
	if asset.State == model.VideoAssetStateDeleted {
		return nil
	}
	if asset.State != model.VideoAssetStateDeleting {
		return nil
	}
	if asset.Kind == model.VideoAssetKindReference {
		inUse, err := videoReferenceAssetInUse(ctx, pipeline.db, asset.ID)
		if err != nil {
			return err
		}
		if inUse {
			return pipeline.db.WithContext(ctx).Model(&model.KKAIVideoAsset{}).
				Where("id = ? AND state = ?", asset.ID, model.VideoAssetStateDeleting).
				Updates(map[string]any{"state": model.VideoAssetStateReady, "updated_at": pipeline.now().Unix()}).Error
		}
	}
	keys := make([]string, 0, 3)
	for _, key := range []string{asset.ObjectKey, asset.PosterObjectKey, asset.PreviewObjectKey} {
		if key != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) > 0 {
		if err := pipeline.store.Delete(ctx, keys); err != nil {
			return err
		}
	}
	now := pipeline.now().Unix()
	return pipeline.db.WithContext(ctx).Model(&model.KKAIVideoAsset{}).Where("id = ?", asset.ID).Updates(map[string]any{
		"state": model.VideoAssetStateDeleted, "archive_source_url": "", "deleted_at": now, "updated_at": now,
	}).Error
}

func (pipeline *VideoAssetPipeline) HandleSamplePrepare(ctx context.Context, event model.KKAIOutboxEvent) error {
	var payload VideoSamplePrepareEventPayload
	if common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.SampleID <= 0 {
		return PermanentKKAIOutboxError(ErrInvalidVideoOutboxEvent)
	}
	var sample model.KKAIVideoSample
	if err := pipeline.db.WithContext(ctx).First(&sample, "id = ?", payload.SampleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var asset model.KKAIVideoAsset
	if err := pipeline.db.WithContext(ctx).First(&asset, "id = ?", sample.VideoAssetID).Error; err != nil {
		return err
	}
	if asset.State == model.VideoAssetStateFailed || asset.State == model.VideoAssetStateDeleted {
		return PermanentKKAIOutboxError(ErrVideoSampleNotPublishable)
	}
	if asset.State != model.VideoAssetStateReady || asset.PosterObjectKey == "" {
		if event.CreatedAt > 0 && pipeline.now().Sub(time.Unix(event.CreatedAt, 0)) >= videoSamplePreparationReadinessTimeout {
			return errors.New("video sample asset preparation readiness timed out")
		}
		return DeferKKAIOutboxUntil(pipeline.now().Add(10*time.Second), errors.New("video sample asset is still preparing"))
	}
	if asset.PreviewObjectKey != "" {
		return nil
	}
	return pipeline.prepareSamplePreview(ctx, &asset)
}

func (pipeline *VideoAssetPipeline) videoAssetFromEvent(ctx context.Context, event model.KKAIOutboxEvent) (*model.KKAIVideoAsset, error) {
	var payload VideoAssetEventPayload
	if common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.AssetID <= 0 {
		return nil, PermanentKKAIOutboxError(ErrInvalidVideoOutboxEvent)
	}
	var asset model.KKAIVideoAsset
	if err := pipeline.db.WithContext(ctx).First(&asset, "id = ?", payload.AssetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &asset, nil
}

func (pipeline *VideoAssetPipeline) copyObjectToTemporaryFile(ctx context.Context, asset model.KKAIVideoAsset) (string, func(), error) {
	object, err := pipeline.store.Get(ctx, asset.ObjectKey)
	if err != nil {
		return "", func() {}, err
	}
	defer object.Body.Close()
	maxBytes := video_studio_setting.Get().MaxArchivedVideoBytes
	if asset.Kind == model.VideoAssetKindReference {
		maxBytes = video_studio_setting.Get().MaxReferenceBytes
	}
	if object.ContentLength <= 0 || object.ContentLength > maxBytes {
		return "", func() {}, ErrVideoArchiveTooLarge
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(object.ContentType, ";")[0]))
	if asset.Kind == model.VideoAssetKindReference {
		if !isSupportedReferenceMIME(mediaType) {
			return "", func() {}, ErrVideoArchiveMIMERejected
		}
	} else if !isSupportedVideoMIME(mediaType) {
		return "", func() {}, ErrVideoArchiveMIMERejected
	}
	available, err := videoTemporaryAvailableBytes(pipeline.tempDir)
	if err != nil || available < uint64(object.ContentLength)+videoTemporaryStorageReserveBytes {
		return "", func() {}, ErrVideoTemporaryStorageUnavailable
	}
	file, err := os.CreateTemp(pipeline.tempDir, "new-api-video-object-*")
	if err != nil {
		return "", func() {}, ErrVideoTemporaryStorageUnavailable
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	written, copyErr := io.Copy(file, io.LimitReader(object.Body, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		cleanup()
		return "", func() {}, copyErr
	}
	if closeErr != nil {
		cleanup()
		return "", func() {}, closeErr
	}
	if written != object.ContentLength || written > maxBytes {
		cleanup()
		return "", func() {}, ErrVideoArchiveTooLarge
	}
	return path, cleanup, nil
}

func (pipeline *VideoAssetPipeline) enqueueAssetEvent(ctx context.Context, assetID int64, topic string, suffix string) error {
	return EnqueueVideoOutboxEvent(ctx, pipeline.db,
		fmt.Sprintf("video:asset:%d:%s", assetID, suffix), topic,
		strconv.FormatInt(assetID, 10), VideoAssetEventPayload{AssetID: assetID},
	)
}

func (pipeline *VideoAssetPipeline) markVideoAssetReady(ctx context.Context, assetID int64) error {
	return pipeline.db.WithContext(ctx).Model(&model.KKAIVideoAsset{}).
		Where("id = ? AND state NOT IN ?", assetID,
			[]string{model.VideoAssetStateDeleting, model.VideoAssetStateDeleted, model.VideoAssetStateFailed}).
		Updates(map[string]any{
			"state": model.VideoAssetStateReady, "failure_reason": "", "updated_at": pipeline.now().Unix(),
		}).Error
}

func (pipeline *VideoAssetPipeline) assetProcessingError(ctx context.Context, assetID int64, err error) error {
	if !isPermanentVideoAssetError(err) {
		return err
	}
	update := pipeline.db.WithContext(ctx).Model(&model.KKAIVideoAsset{}).
		Where("id = ? AND state NOT IN ?", assetID,
			[]string{model.VideoAssetStateDeleting, model.VideoAssetStateDeleted}).
		Updates(map[string]any{
			"state": model.VideoAssetStateFailed, "failure_reason": videoAssetFailureReason(err),
			"archive_source_url": "", "updated_at": pipeline.now().Unix(),
		})
	if update.Error != nil {
		return update.Error
	}
	return PermanentKKAIOutboxError(err)
}

func isPermanentVideoAssetError(err error) bool {
	return errors.Is(err, ErrVideoArchiveSourceRejected) || errors.Is(err, ErrVideoArchiveResponseRejected) ||
		errors.Is(err, ErrVideoArchiveMIMERejected) || errors.Is(err, ErrVideoArchiveTooLarge) ||
		errors.Is(err, ErrVideoMediaInvalid) || errors.Is(err, ErrVideoMediaProcessingFailed)
}

func videoAssetFailureReason(err error) string {
	switch {
	case errors.Is(err, ErrVideoArchiveSourceRejected):
		return "archive source rejected"
	case errors.Is(err, ErrVideoArchiveResponseRejected):
		return "archive source response rejected"
	case errors.Is(err, ErrVideoArchiveMIMERejected):
		return "archive media type rejected"
	case errors.Is(err, ErrVideoArchiveTooLarge):
		return "archive exceeds size limit"
	default:
		return "media processing failed"
	}
}

func videoAssetDerivedPath(objectKey string, suffix string) string {
	return strings.TrimSuffix(objectKey, filepath.Ext(objectKey)) + suffix
}
