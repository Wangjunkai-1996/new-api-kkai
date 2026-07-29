package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"

	"gorm.io/gorm"
)

const (
	videoTaskArchiveOnceLockTimeout = 30 * time.Second
	videoOutboxTopicArchiveOnce     = "video.asset.archive.once.v1"
	videoOutboxTopicInspectOnce     = "video.asset.inspect.once.v1"
	videoOutboxTopicPosterOnce      = "video.asset.poster.once.v1"
)

var (
	ErrVideoTaskArchiveOnceInvalidInput = errors.New("invalid exact video archive input")
	ErrVideoTaskArchiveOnceMismatch     = errors.New("exact video archive coordinates do not match")
	ErrVideoTaskArchiveOnceBlocked      = errors.New("exact video archive is blocked by existing state")
	ErrVideoTaskArchiveOnceCorrupt      = errors.New("exact video archive state is inconsistent")
)

type VideoTaskArchiveOnceStage string

const (
	VideoTaskArchiveOnceStageAwaitingArchive VideoTaskArchiveOnceStage = "awaiting_archive"
	VideoTaskArchiveOnceStageAwaitingInspect VideoTaskArchiveOnceStage = "awaiting_inspect"
	VideoTaskArchiveOnceStageAwaitingPoster  VideoTaskArchiveOnceStage = "awaiting_poster"
	VideoTaskArchiveOnceStageReady           VideoTaskArchiveOnceStage = "ready"
)

type VideoTaskArchiveOnceInput struct {
	TaskID                 int64  `json:"task_id"`
	GenerationID           int64  `json:"generation_id"`
	ExpectedUserID         int    `json:"expected_user_id"`
	ExpectedExternalTaskID string `json:"expected_external_task_id"`
}

type VideoTaskArchiveOnceResult struct {
	TaskID       int64                     `json:"task_id"`
	GenerationID int64                     `json:"generation_id"`
	AssetID      int64                     `json:"asset_id,omitempty"`
	Stage        VideoTaskArchiveOnceStage `json:"stage"`
}

type VideoTaskArchiveOnceExecutor struct {
	db        *gorm.DB
	store     VideoAssetStore
	pipeline  *VideoAssetPipeline
	processor *KKAIOutboxProcessor
	tempDir   string
}

type videoTaskArchiveOnceSnapshot struct {
	result VideoTaskArchiveOnceResult
	asset  *model.KKAIVideoAsset
}

type videoTaskArchiveOnceEventExpectation struct {
	key         string
	topic       string
	aggregateID string
	assetID     int64
}

func NewVideoTaskArchiveOnceExecutor(
	db *gorm.DB,
	store VideoAssetStore,
	media VideoMediaProcessor,
	fetcher VideoArchiveSourceFetcher,
	tempDir string,
	workerID string,
) (*VideoTaskArchiveOnceExecutor, error) {
	workerID = strings.TrimSpace(workerID)
	if db == nil || workerID == "" {
		return nil, ErrVideoTaskArchiveOnceInvalidInput
	}
	pipeline, err := NewVideoAssetPipeline(db, store, media, fetcher, tempDir)
	if err != nil {
		return nil, err
	}
	pipeline.inspectTopic = videoOutboxTopicInspectOnce
	pipeline.posterTopic = videoOutboxTopicPosterOnce
	processor := NewKKAIOutboxProcessor(db, workerID)
	processor.lockTimeout = videoTaskArchiveOnceLockTimeout
	for topic, handler := range map[string]KKAIOutboxHandler{
		videoOutboxTopicArchiveOnce: pipeline.HandleArchive,
		videoOutboxTopicInspectOnce: pipeline.HandleInspect,
		videoOutboxTopicPosterOnce:  pipeline.HandlePoster,
	} {
		if err := processor.Register(topic, handler); err != nil {
			return nil, err
		}
	}
	return &VideoTaskArchiveOnceExecutor{
		db: db, store: store, pipeline: pipeline, processor: processor, tempDir: pipeline.tempDir,
	}, nil
}

func PreviewVideoTaskArchiveOnce(
	ctx context.Context,
	db *gorm.DB,
	input VideoTaskArchiveOnceInput,
) (VideoTaskArchiveOnceResult, error) {
	if !videoTaskArchiveOnceBackgroundJobsDisabled() {
		return VideoTaskArchiveOnceResult{}, ErrVideoTaskArchiveOnceBlocked
	}
	snapshot, err := inspectVideoTaskArchiveOnce(ctx, db, input, time.Now())
	if err != nil {
		return VideoTaskArchiveOnceResult{}, err
	}
	return snapshot.result, nil
}

func (executor *VideoTaskArchiveOnceExecutor) Execute(
	ctx context.Context,
	input VideoTaskArchiveOnceInput,
) (VideoTaskArchiveOnceResult, error) {
	if executor == nil || executor.db == nil || executor.store == nil || executor.pipeline == nil || executor.processor == nil {
		return VideoTaskArchiveOnceResult{}, ErrVideoTaskArchiveOnceInvalidInput
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !videoTaskArchiveOnceBackgroundJobsDisabled() {
		return VideoTaskArchiveOnceResult{}, ErrVideoTaskArchiveOnceBlocked
	}
	snapshot, err := inspectVideoTaskArchiveOnce(ctx, executor.db, input, executor.processor.now())
	if err != nil {
		return VideoTaskArchiveOnceResult{}, err
	}
	if err := executor.verifyObjects(ctx, snapshot); err != nil {
		return snapshot.result, err
	}
	if snapshot.result.Stage == VideoTaskArchiveOnceStageReady {
		return snapshot.result, nil
	}
	if err := executor.requireTemporaryCapacity(snapshot); err != nil {
		return snapshot.result, err
	}
	if snapshot.result.AssetID == 0 {
		if _, err := reconcileVideoTaskOutputExpected(ctx, executor.db, input.GenerationID, &input); err != nil {
			return VideoTaskArchiveOnceResult{}, err
		}
	}

	for processed := 0; processed < 3; processed++ {
		snapshot, err = inspectVideoTaskArchiveOnce(ctx, executor.db, input, executor.processor.now())
		if err != nil {
			return VideoTaskArchiveOnceResult{}, err
		}
		if err := executor.verifyObjects(ctx, snapshot); err != nil {
			return snapshot.result, err
		}
		if snapshot.result.Stage == VideoTaskArchiveOnceStageReady {
			return snapshot.result, nil
		}
		if snapshot.result.AssetID <= 0 {
			return snapshot.result, ErrVideoTaskArchiveOnceCorrupt
		}
		if err := executor.requireTemporaryCapacity(snapshot); err != nil {
			return snapshot.result, err
		}
		expectation := videoTaskArchiveOnceEventForStage(snapshot.result)
		if expectation.key == "" {
			return snapshot.result, ErrVideoTaskArchiveOnceCorrupt
		}
		if err := executor.processor.processExactVideoTaskArchiveEvent(ctx, input, expectation); err != nil {
			return snapshot.result, err
		}
	}

	snapshot, err = inspectVideoTaskArchiveOnce(ctx, executor.db, input, executor.processor.now())
	if err != nil {
		return VideoTaskArchiveOnceResult{}, err
	}
	if err := executor.verifyObjects(ctx, snapshot); err != nil {
		return snapshot.result, err
	}
	if snapshot.result.Stage != VideoTaskArchiveOnceStageReady {
		return snapshot.result, ErrVideoTaskArchiveOnceBlocked
	}
	return snapshot.result, nil
}

func videoTaskArchiveOnceBackgroundJobsDisabled() bool {
	settings := video_studio_setting.Get()
	return !settings.ArchiveEnqueueEnabled && !settings.BackfillEnabled && !settings.WorkerEnabled
}

func (executor *VideoTaskArchiveOnceExecutor) verifyObjects(
	ctx context.Context,
	snapshot videoTaskArchiveOnceSnapshot,
) error {
	if snapshot.asset == nil {
		return nil
	}
	asset := *snapshot.asset
	metadata, err := executor.store.Head(ctx, asset.ObjectKey)
	if asset.ArchiveSourceURL != "" {
		if errors.Is(err, ErrVideoAssetObjectNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		resolved, err := executor.pipeline.resolveVideoArchiveSource(ctx, asset)
		if err != nil {
			return ErrVideoTaskArchiveOnceBlocked
		}
		expectedFingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(resolved.fetch.Source))))
		if !validArchivedVideoObject(metadata, video_studio_setting.Get().MaxArchivedVideoBytes) ||
			metadata.ArchiveSourceSHA256 != expectedFingerprint {
			return ErrVideoTaskArchiveOnceCorrupt
		}
	} else {
		if err != nil {
			if errors.Is(err, ErrVideoAssetObjectNotFound) {
				return ErrVideoTaskArchiveOnceCorrupt
			}
			return err
		}
		if !validArchivedVideoObject(metadata, video_studio_setting.Get().MaxArchivedVideoBytes) ||
			metadata.ContentLength != asset.SizeBytes ||
			normalizedVideoObjectContentType(metadata.ContentType) != normalizedVideoObjectContentType(asset.MIMEType) ||
			strings.ToLower(metadata.SHA256) != strings.ToLower(asset.SHA256) {
			return ErrVideoTaskArchiveOnceCorrupt
		}
	}
	if asset.PosterObjectKey == "" {
		return nil
	}
	poster, err := executor.store.Head(ctx, asset.PosterObjectKey)
	if err != nil {
		if errors.Is(err, ErrVideoAssetObjectNotFound) {
			return ErrVideoTaskArchiveOnceCorrupt
		}
		return err
	}
	if normalizedVideoObjectContentType(poster.ContentType) != "image/jpeg" ||
		poster.ContentLength <= 0 || poster.ContentLength > videoPosterMaximumBytes {
		return ErrVideoTaskArchiveOnceCorrupt
	}
	return nil
}

func (executor *VideoTaskArchiveOnceExecutor) requireTemporaryCapacity(snapshot videoTaskArchiveOnceSnapshot) error {
	required := uint64(video_studio_setting.Get().MaxArchivedVideoBytes) + videoTemporaryStorageReserveBytes
	if snapshot.asset != nil && snapshot.asset.ArchiveSourceURL == "" && snapshot.asset.SizeBytes > 0 {
		required = uint64(snapshot.asset.SizeBytes) + videoTemporaryStorageReserveBytes
	}
	available, err := videoTemporaryAvailableBytes(executor.tempDir)
	if err != nil || available < required {
		return ErrVideoTemporaryStorageUnavailable
	}
	return nil
}

func inspectVideoTaskArchiveOnce(
	ctx context.Context,
	db *gorm.DB,
	input VideoTaskArchiveOnceInput,
	now time.Time,
) (videoTaskArchiveOnceSnapshot, error) {
	if db == nil || !validVideoTaskArchiveOnceInput(input) {
		return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceInvalidInput
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var generation model.KKAIVideoGeneration
	if err := db.WithContext(ctx).First(&generation, "id = ?", input.GenerationID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceMismatch
		}
		return videoTaskArchiveOnceSnapshot{}, err
	}
	var task model.Task
	if err := db.WithContext(ctx).First(&task, "id = ?", input.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceMismatch
		}
		return videoTaskArchiveOnceSnapshot{}, err
	}
	if err := validateVideoTaskArchiveOncePair(input, generation, task); err != nil {
		return videoTaskArchiveOnceSnapshot{}, err
	}

	result := VideoTaskArchiveOnceResult{
		TaskID: input.TaskID, GenerationID: input.GenerationID,
		Stage: VideoTaskArchiveOnceStageAwaitingArchive,
	}
	var links []model.KKAIVideoTaskAsset
	if err := db.WithContext(ctx).Where(
		"task_id = ? AND role = ?", input.TaskID, model.VideoTaskAssetRoleOutput,
	).Order("position ASC").Find(&links).Error; err != nil {
		return videoTaskArchiveOnceSnapshot{}, err
	}
	if len(links) == 0 {
		if videoTaskArchiveSource(task) == "" {
			return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceBlocked
		}
		var eventCount int64
		archiveKey := fmt.Sprintf("video:task:%d:archive:v1", input.TaskID)
		if err := db.WithContext(ctx).Model(&model.KKAIOutboxEvent{}).
			Where("event_key = ?", archiveKey).Count(&eventCount).Error; err != nil {
			return videoTaskArchiveOnceSnapshot{}, err
		}
		if eventCount != 0 {
			return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceCorrupt
		}
		return videoTaskArchiveOnceSnapshot{result: result}, nil
	}
	if len(links) != 1 || links[0].Position != 0 || links[0].AssetID <= 0 {
		return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceCorrupt
	}

	var asset model.KKAIVideoAsset
	if err := db.WithContext(ctx).First(&asset, "id = ?", links[0].AssetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceCorrupt
		}
		return videoTaskArchiveOnceSnapshot{}, err
	}
	if err := validateVideoTaskArchiveOnceAsset(input, task, asset); err != nil {
		return videoTaskArchiveOnceSnapshot{}, err
	}
	result.AssetID = asset.ID

	archiveExpectation := videoTaskArchiveOnceEventExpectation{
		key: fmt.Sprintf("video:task:%d:archive:v1", input.TaskID), topic: videoOutboxTopicArchiveOnce,
		aggregateID: strconv.FormatInt(asset.ID, 10), assetID: asset.ID,
	}
	inspectExpectation := videoTaskArchiveOnceEventExpectation{
		key: fmt.Sprintf("video:asset:%d:inspect:v1", asset.ID), topic: videoOutboxTopicInspectOnce,
		aggregateID: strconv.FormatInt(asset.ID, 10), assetID: asset.ID,
	}
	posterExpectation := videoTaskArchiveOnceEventExpectation{
		key: fmt.Sprintf("video:asset:%d:poster:v1", asset.ID), topic: videoOutboxTopicPosterOnce,
		aggregateID: strconv.FormatInt(asset.ID, 10), assetID: asset.ID,
	}
	archiveEvent, archiveExists, err := loadOptionalVideoTaskArchiveOnceEvent(
		ctx, db, archiveExpectation, now, videoTaskArchiveOnceLockTimeout,
	)
	if err != nil {
		return videoTaskArchiveOnceSnapshot{}, err
	}
	inspectEvent, inspectExists, err := loadOptionalVideoTaskArchiveOnceEvent(
		ctx, db, inspectExpectation, now, videoTaskArchiveOnceLockTimeout,
	)
	if err != nil {
		return videoTaskArchiveOnceSnapshot{}, err
	}
	posterEvent, posterExists, err := loadOptionalVideoTaskArchiveOnceEvent(
		ctx, db, posterExpectation, now, videoTaskArchiveOnceLockTimeout,
	)
	if err != nil {
		return videoTaskArchiveOnceSnapshot{}, err
	}
	if !archiveExists || (!inspectExists && posterExists) {
		return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceCorrupt
	}
	if asset.ArchiveSourceURL == "" {
		if err := validateVideoTaskArchiveOnceArchivedAsset(asset); err != nil {
			return videoTaskArchiveOnceSnapshot{}, err
		}
	}
	if archiveEvent.Status == model.KKAIOutboxStatusPending {
		return videoTaskArchiveOnceSnapshot{result: result, asset: &asset}, nil
	}
	if asset.ArchiveSourceURL != "" {
		return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceCorrupt
	}
	if !inspectExists {
		return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceCorrupt
	}
	if inspectEvent.Status == model.KKAIOutboxStatusPending {
		if posterExists && (asset.Width <= 0 || asset.Height <= 0 || asset.DurationSeconds <= 0 || strings.TrimSpace(asset.Codec) == "") {
			return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceCorrupt
		}
		result.Stage = VideoTaskArchiveOnceStageAwaitingInspect
		return videoTaskArchiveOnceSnapshot{result: result, asset: &asset}, nil
	}
	if asset.Width <= 0 || asset.Height <= 0 || asset.DurationSeconds <= 0 || strings.TrimSpace(asset.Codec) == "" {
		return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceCorrupt
	}

	if !posterExists {
		return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceCorrupt
	}
	if posterEvent.Status == model.KKAIOutboxStatusPending {
		result.Stage = VideoTaskArchiveOnceStageAwaitingPoster
		return videoTaskArchiveOnceSnapshot{result: result, asset: &asset}, nil
	}
	expectedPosterKey := videoAssetDerivedPath(asset.ObjectKey, ".poster.jpg")
	if asset.State != model.VideoAssetStateReady || asset.PosterObjectKey != expectedPosterKey {
		return videoTaskArchiveOnceSnapshot{}, ErrVideoTaskArchiveOnceCorrupt
	}
	result.Stage = VideoTaskArchiveOnceStageReady
	return videoTaskArchiveOnceSnapshot{result: result, asset: &asset}, nil
}

func validateVideoTaskArchiveOnceArchivedAsset(asset model.KKAIVideoAsset) error {
	if asset.SizeBytes <= 0 || !isSupportedVideoMIME(normalizedVideoObjectContentType(asset.MIMEType)) ||
		!validSHA256Hex(strings.ToLower(asset.SHA256)) {
		return ErrVideoTaskArchiveOnceCorrupt
	}
	return nil
}

func validVideoTaskArchiveOnceInput(input VideoTaskArchiveOnceInput) bool {
	return input.TaskID > 0 && input.GenerationID > 0 && input.ExpectedUserID > 0 &&
		input.ExpectedExternalTaskID != "" && input.ExpectedExternalTaskID == strings.TrimSpace(input.ExpectedExternalTaskID)
}

func validateVideoTaskArchiveOncePair(
	input VideoTaskArchiveOnceInput,
	generation model.KKAIVideoGeneration,
	task model.Task,
) error {
	if !validVideoTaskArchiveOnceInput(input) {
		return ErrVideoTaskArchiveOnceInvalidInput
	}
	if generation.ID != input.GenerationID || generation.TaskID != input.TaskID || generation.UserID != input.ExpectedUserID ||
		task.ID != input.TaskID || task.UserId != input.ExpectedUserID || task.TaskID != input.ExpectedExternalTaskID {
		return ErrVideoTaskArchiveOnceMismatch
	}
	if generation.DeletedAt != 0 || task.Status != model.TaskStatusSuccess || strings.TrimSpace(task.Progress) != "100%" {
		return ErrVideoTaskArchiveOnceBlocked
	}
	return nil
}

func validateVideoTaskArchiveOnceAsset(
	input VideoTaskArchiveOnceInput,
	task model.Task,
	asset model.KKAIVideoAsset,
) error {
	expectedObjectKey := fmt.Sprintf("users/%d/generations/%d/source.mp4", input.ExpectedUserID, input.GenerationID)
	expectedArchiveSource := videoTaskResultArchiveSource(input.TaskID)
	expectedPosterKey := videoAssetDerivedPath(expectedObjectKey, ".poster.jpg")
	if asset.OwnerUserID != input.ExpectedUserID || asset.Scope != model.VideoAssetScopeUser || asset.Kind != model.VideoAssetKindOutput ||
		asset.ObjectKey != expectedObjectKey || asset.DeletedAt != 0 || asset.PreviewObjectKey != "" ||
		(asset.ArchiveSourceURL != "" && asset.ArchiveSourceURL != expectedArchiveSource) ||
		(asset.PosterObjectKey != "" && asset.PosterObjectKey != expectedPosterKey) ||
		strings.TrimSpace(asset.FailureReason) != "" || !task.PrivateData.AssetHostedResult {
		return ErrVideoTaskArchiveOnceCorrupt
	}
	if asset.State != model.VideoAssetStateProcessing && asset.State != model.VideoAssetStateReady {
		return ErrVideoTaskArchiveOnceBlocked
	}
	if (asset.State == model.VideoAssetStateReady && (asset.ArchiveSourceURL != "" || asset.PosterObjectKey == "")) ||
		(asset.State == model.VideoAssetStateProcessing && asset.PosterObjectKey != "") {
		return ErrVideoTaskArchiveOnceCorrupt
	}
	if asset.ArchiveSourceURL == expectedArchiveSource && videoTaskArchiveSource(task) == "" {
		return ErrVideoTaskArchiveOnceCorrupt
	}
	return nil
}

func loadOptionalVideoTaskArchiveOnceEvent(
	ctx context.Context,
	db *gorm.DB,
	expectation videoTaskArchiveOnceEventExpectation,
	now time.Time,
	lockTimeout time.Duration,
) (model.KKAIOutboxEvent, bool, error) {
	var event model.KKAIOutboxEvent
	if err := db.WithContext(ctx).First(&event, "event_key = ?", expectation.key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.KKAIOutboxEvent{}, false, nil
		}
		return model.KKAIOutboxEvent{}, false, err
	}
	if err := validateVideoTaskArchiveOnceEvent(event, expectation, now, lockTimeout); err != nil {
		return model.KKAIOutboxEvent{}, false, err
	}
	return event, true, nil
}

func validateVideoTaskArchiveOnceEvent(
	event model.KKAIOutboxEvent,
	expectation videoTaskArchiveOnceEventExpectation,
	now time.Time,
	lockTimeout time.Duration,
) error {
	var payload VideoAssetEventPayload
	if event.EventKey != expectation.key || event.Topic != expectation.topic || event.AggregateID != expectation.aggregateID ||
		common.UnmarshalJsonStr(event.Payload, &payload) != nil || payload.AssetID != expectation.assetID {
		return ErrVideoTaskArchiveOnceCorrupt
	}
	switch event.Status {
	case model.KKAIOutboxStatusDelivered:
		if event.LockedAt != 0 || event.LockedBy != "" {
			return ErrVideoTaskArchiveOnceCorrupt
		}
		return nil
	case model.KKAIOutboxStatusDead:
		return ErrVideoTaskArchiveOnceBlocked
	case model.KKAIOutboxStatusPending:
	default:
		return ErrVideoTaskArchiveOnceCorrupt
	}
	if event.Attempts != 0 || event.AvailableAt <= 0 || event.AvailableAt > now.Unix() {
		return ErrVideoTaskArchiveOnceBlocked
	}
	if (event.LockedAt == 0) != (event.LockedBy == "") {
		return ErrVideoTaskArchiveOnceCorrupt
	}
	if event.LockedAt > now.Add(-lockTimeout).Unix() {
		return ErrVideoTaskArchiveOnceBlocked
	}
	return nil
}

func videoTaskArchiveOnceEventForStage(result VideoTaskArchiveOnceResult) videoTaskArchiveOnceEventExpectation {
	aggregateID := strconv.FormatInt(result.AssetID, 10)
	switch result.Stage {
	case VideoTaskArchiveOnceStageAwaitingArchive:
		return videoTaskArchiveOnceEventExpectation{
			key: fmt.Sprintf("video:task:%d:archive:v1", result.TaskID), topic: videoOutboxTopicArchiveOnce,
			aggregateID: aggregateID, assetID: result.AssetID,
		}
	case VideoTaskArchiveOnceStageAwaitingInspect:
		return videoTaskArchiveOnceEventExpectation{
			key: fmt.Sprintf("video:asset:%d:inspect:v1", result.AssetID), topic: videoOutboxTopicInspectOnce,
			aggregateID: aggregateID, assetID: result.AssetID,
		}
	case VideoTaskArchiveOnceStageAwaitingPoster:
		return videoTaskArchiveOnceEventExpectation{
			key: fmt.Sprintf("video:asset:%d:poster:v1", result.AssetID), topic: videoOutboxTopicPosterOnce,
			aggregateID: aggregateID, assetID: result.AssetID,
		}
	default:
		return videoTaskArchiveOnceEventExpectation{}
	}
}

func (processor *KKAIOutboxProcessor) processExactVideoTaskArchiveEvent(
	ctx context.Context,
	input VideoTaskArchiveOnceInput,
	expectation videoTaskArchiveOnceEventExpectation,
) error {
	if processor == nil || processor.db == nil || processor.workerID == "" || expectation.key == "" || expectation.assetID <= 0 {
		return ErrVideoTaskArchiveOnceInvalidInput
	}
	now := processor.now()
	event, claimed, err := processor.claimExactVideoTaskArchiveEvent(ctx, input, expectation, now)
	if err != nil || !claimed {
		return err
	}
	result, err := processor.processClaimedEvent(ctx, event)
	if err != nil {
		return err
	}
	if result.Delivered != 1 || result.Deferred != 0 || result.Retried != 0 || result.Dead != 0 {
		return ErrVideoTaskArchiveOnceBlocked
	}
	return nil
}

func (processor *KKAIOutboxProcessor) claimExactVideoTaskArchiveEvent(
	ctx context.Context,
	input VideoTaskArchiveOnceInput,
	expectation videoTaskArchiveOnceEventExpectation,
	now time.Time,
) (model.KKAIOutboxEvent, bool, error) {
	claim := func(tx *gorm.DB) (model.KKAIOutboxEvent, bool, error) {
		asset, err := lockVideoTaskArchiveOnceCoordinates(ctx, tx, input, expectation)
		if err != nil {
			return model.KKAIOutboxEvent{}, false, err
		}
		expectations := videoTaskArchiveOnceEventExpectations(input.TaskID, asset.ID)
		keys := make([]string, 0, len(expectations))
		expectedByKey := make(map[string]videoTaskArchiveOnceEventExpectation, len(expectations))
		for _, expected := range expectations {
			keys = append(keys, expected.key)
			expectedByKey[expected.key] = expected
		}
		var events []model.KKAIOutboxEvent
		query := tx.WithContext(ctx).Where("event_key IN ?", keys).Order("id ASC")
		if tx.Dialector.Name() != "sqlite" {
			query = lockVideoRowsForUpdate(query)
		}
		if err := query.Find(&events).Error; err != nil {
			return model.KKAIOutboxEvent{}, false, err
		}
		eventsByKey := make(map[string]model.KKAIOutboxEvent, len(events))
		for _, candidate := range events {
			expected, ok := expectedByKey[candidate.EventKey]
			if !ok {
				return model.KKAIOutboxEvent{}, false, ErrVideoTaskArchiveOnceCorrupt
			}
			if err := validateVideoTaskArchiveOnceEvent(candidate, expected, now, processor.lockTimeout); err != nil {
				return model.KKAIOutboxEvent{}, false, err
			}
			eventsByKey[candidate.EventKey] = candidate
		}
		event, ok := eventsByKey[expectation.key]
		if !ok {
			return model.KKAIOutboxEvent{}, false, ErrVideoTaskArchiveOnceCorrupt
		}
		for _, predecessor := range expectations {
			if predecessor.key == expectation.key {
				break
			}
			persisted, ok := eventsByKey[predecessor.key]
			if !ok || persisted.Status != model.KKAIOutboxStatusDelivered {
				return model.KKAIOutboxEvent{}, false, ErrVideoTaskArchiveOnceBlocked
			}
		}
		if event.Status == model.KKAIOutboxStatusDelivered {
			return event, false, nil
		}
		staleBefore := now.Add(-processor.lockTimeout).Unix()
		fence := processor.newKKAIOutboxFence()
		update := tx.WithContext(ctx).Model(&model.KKAIOutboxEvent{}).
			Where(
				"id = ? AND event_key = ? AND topic = ? AND aggregate_id = ? AND payload = ? AND status = ? AND attempts = ? AND available_at <= ? AND (locked_at = 0 OR locked_at <= ?)",
				event.ID, expectation.key, expectation.topic, expectation.aggregateID, event.Payload,
				model.KKAIOutboxStatusPending, 0, now.Unix(), staleBefore,
			).
			Updates(map[string]any{"locked_at": now.Unix(), "locked_by": fence})
		if update.Error != nil {
			return model.KKAIOutboxEvent{}, false, update.Error
		}
		if update.RowsAffected != 1 {
			return model.KKAIOutboxEvent{}, false, ErrVideoTaskArchiveOnceBlocked
		}
		event.LockedAt = now.Unix()
		event.LockedBy = fence
		return event, true, nil
	}

	var event model.KKAIOutboxEvent
	var claimed bool
	// The exact claim starts its lease only after this transaction commits, so SQLite
	// can keep the coordinate snapshot and event CAS in one fail-closed transaction.
	err := processor.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		event, claimed, err = claim(tx)
		return err
	})
	return event, claimed, err
}

func lockVideoTaskArchiveOnceCoordinates(
	ctx context.Context,
	tx *gorm.DB,
	input VideoTaskArchiveOnceInput,
	expectation videoTaskArchiveOnceEventExpectation,
) (model.KKAIVideoAsset, error) {
	var generation model.KKAIVideoGeneration
	if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).First(&generation, "id = ?", input.GenerationID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.KKAIVideoAsset{}, ErrVideoTaskArchiveOnceMismatch
		}
		return model.KKAIVideoAsset{}, err
	}
	var task model.Task
	if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).First(&task, "id = ?", generation.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.KKAIVideoAsset{}, ErrVideoTaskArchiveOnceMismatch
		}
		return model.KKAIVideoAsset{}, err
	}
	if err := validateVideoTaskArchiveOncePair(input, generation, task); err != nil {
		return model.KKAIVideoAsset{}, err
	}
	var links []model.KKAIVideoTaskAsset
	if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).Where(
		"task_id = ? AND role = ?", input.TaskID, model.VideoTaskAssetRoleOutput,
	).Order("position ASC").Find(&links).Error; err != nil {
		return model.KKAIVideoAsset{}, err
	}
	if len(links) != 1 || links[0].Position != 0 || links[0].AssetID != expectation.assetID {
		return model.KKAIVideoAsset{}, ErrVideoTaskArchiveOnceCorrupt
	}
	var asset model.KKAIVideoAsset
	if err := lockVideoRowsForUpdate(tx.WithContext(ctx)).First(&asset, "id = ?", links[0].AssetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.KKAIVideoAsset{}, ErrVideoTaskArchiveOnceCorrupt
		}
		return model.KKAIVideoAsset{}, err
	}
	if err := validateVideoTaskArchiveOnceAsset(input, task, asset); err != nil {
		return model.KKAIVideoAsset{}, err
	}
	expected := videoTaskArchiveOnceExpectationForTopic(input.TaskID, asset.ID, expectation.topic)
	if expected != expectation {
		return model.KKAIVideoAsset{}, ErrVideoTaskArchiveOnceCorrupt
	}
	switch expectation.topic {
	case videoOutboxTopicArchiveOnce:
	case videoOutboxTopicInspectOnce:
		if asset.ArchiveSourceURL != "" {
			return model.KKAIVideoAsset{}, ErrVideoTaskArchiveOnceCorrupt
		}
		if err := validateVideoTaskArchiveOnceArchivedAsset(asset); err != nil {
			return model.KKAIVideoAsset{}, err
		}
	case videoOutboxTopicPosterOnce:
		if asset.ArchiveSourceURL != "" {
			return model.KKAIVideoAsset{}, ErrVideoTaskArchiveOnceCorrupt
		}
		if err := validateVideoTaskArchiveOnceArchivedAsset(asset); err != nil {
			return model.KKAIVideoAsset{}, err
		}
		if asset.Width <= 0 || asset.Height <= 0 || asset.DurationSeconds <= 0 || strings.TrimSpace(asset.Codec) == "" {
			return model.KKAIVideoAsset{}, ErrVideoTaskArchiveOnceCorrupt
		}
	default:
		return model.KKAIVideoAsset{}, ErrVideoTaskArchiveOnceCorrupt
	}
	return asset, nil
}

func videoTaskArchiveOnceEventExpectations(taskID int64, assetID int64) []videoTaskArchiveOnceEventExpectation {
	aggregateID := strconv.FormatInt(assetID, 10)
	return []videoTaskArchiveOnceEventExpectation{
		{
			key: fmt.Sprintf("video:task:%d:archive:v1", taskID), topic: videoOutboxTopicArchiveOnce,
			aggregateID: aggregateID, assetID: assetID,
		},
		{
			key: fmt.Sprintf("video:asset:%d:inspect:v1", assetID), topic: videoOutboxTopicInspectOnce,
			aggregateID: aggregateID, assetID: assetID,
		},
		{
			key: fmt.Sprintf("video:asset:%d:poster:v1", assetID), topic: videoOutboxTopicPosterOnce,
			aggregateID: aggregateID, assetID: assetID,
		},
	}
}

func videoTaskArchiveOnceExpectationForTopic(
	taskID int64,
	assetID int64,
	topic string,
) videoTaskArchiveOnceEventExpectation {
	for _, expectation := range videoTaskArchiveOnceEventExpectations(taskID, assetID) {
		if expectation.topic == topic {
			return expectation
		}
	}
	return videoTaskArchiveOnceEventExpectation{}
}
