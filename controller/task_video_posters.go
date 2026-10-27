package controller

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func PrepareTaskVideoPosters(c *gin.Context)      { prepareTaskVideoPosters(c, false) }
func AdminPrepareTaskVideoPosters(c *gin.Context) { prepareTaskVideoPosters(c, true) }

func prepareTaskVideoPosters(c *gin.Context, admin bool) {
	c.Header("Cache-Control", "private, no-store")
	var request struct {
		TaskRecordIDs []int64 `json:"task_record_ids"`
		ArtifactKey   string  `json:"artifact_key"`
	}
	if c.ShouldBindJSON(&request) != nil || len(request.TaskRecordIDs) == 0 || len(request.TaskRecordIDs) > 20 ||
		request.ArtifactKey != "" && (len(request.TaskRecordIDs) != 1 || !taskArtifactKeyPattern.MatchString(request.ArtifactKey)) {
		writeTaskArtifactError(c, http.StatusBadRequest, "invalid_poster_request", "Invalid video poster request")
		return
	}
	for _, id := range request.TaskRecordIDs {
		if id <= 0 {
			writeTaskArtifactError(c, http.StatusBadRequest, "invalid_poster_request", "Invalid video poster request")
			return
		}
	}
	if !service.TaskVideoPostersEnabled() {
		writeTaskArtifactError(c, http.StatusServiceUnavailable, "poster_service_unavailable", "Video posters are temporarily unavailable")
		return
	}
	store, err := videoStudioAssetStore(c)
	if err != nil {
		writeTaskArtifactError(c, http.StatusServiceUnavailable, "poster_service_unavailable", "Video posters are temporarily unavailable")
		return
	}
	if _, err := service.TaskVideoPosterProcessor(c.Request.Context()); err != nil {
		writeTaskArtifactError(c, http.StatusServiceUnavailable, "poster_service_unavailable", "Video posters are temporarily unavailable")
		return
	}
	var tasks []*model.Task
	query := model.DB.WithContext(c.Request.Context()).Where("id IN ?", request.TaskRecordIDs)
	if !admin {
		query = query.Where("user_id = ?", c.GetInt("id"))
	}
	if err := query.Find(&tasks).Error; err != nil {
		writeTaskArtifactError(c, http.StatusInternalServerError, "poster_query_failed", "Failed to load video posters")
		return
	}
	allowed, err := authorizedTaskVideoPosterOwners(c.Request.Context(), model.DB, tasks, c.GetInt("id"), admin)
	if err != nil {
		writeTaskArtifactError(c, http.StatusInternalServerError, "poster_query_failed", "Failed to load video posters")
		return
	}
	// Batch-check Studio membership; ordinary poster preparation never changes
	// its ownership/archival boundary or falls back to a private provider URL.
	var hosted []model.KKAIVideoGeneration
	if err := model.DB.WithContext(c.Request.Context()).Select("task_id").Where("task_id IN ?", request.TaskRecordIDs).Find(&hosted).Error; err != nil {
		writeTaskArtifactError(c, http.StatusInternalServerError, "poster_query_failed", "Failed to load video posters")
		return
	}
	for _, generation := range hosted {
		delete(allowed, generation.TaskID)
	}
	type item struct {
		ID     int64                `json:"id"`
		Poster *dto.TaskVideoPoster `json:"poster,omitempty"`
	}
	items := make([]item, 0, len(tasks))
	failed := make(map[int64]*dto.TaskVideoPoster)
	primaryKeys := make(map[int64]string)
	for _, task := range tasks {
		if !allowed[task.ID] {
			continue
		}
		keys, keyErr := taskVideoPosterArtifactKeys(task)
		if keyErr != nil {
			failed[task.ID] = &dto.TaskVideoPoster{State: "unavailable", FailureCode: "source_unavailable"}
			continue
		}
		if request.ArtifactKey != "" {
			found := false
			for _, key := range keys {
				found = found || key == request.ArtifactKey
			}
			if !found {
				failed[task.ID] = &dto.TaskVideoPoster{State: "unavailable", FailureCode: "source_unavailable"}
				continue
			}
			keys = []string{request.ArtifactKey}
		} else if len(keys) > 1 {
			keys = keys[:1]
		}
		if len(keys) == 0 {
			continue
		}
		primaryKeys[task.ID] = keys[0]
		if err := service.QueueTaskVideoPoster(c.Request.Context(), model.DB, task, keys[0]); err != nil {
			writeTaskArtifactError(c, http.StatusInternalServerError, "poster_prepare_failed", "Failed to prepare video posters")
			return
		}
	}
	// Fresh snapshots include concurrent worker completion and renewed leases.
	if err := query.Find(&tasks).Error; err != nil {
		writeTaskArtifactError(c, http.StatusInternalServerError, "poster_query_failed", "Failed to load video posters")
		return
	}
	for _, task := range tasks {
		if allowed[task.ID] {
			if poster := failed[task.ID]; poster != nil {
				items = append(items, item{ID: task.ID, Poster: poster})
			} else if poster := service.TaskVideoPosterForDisplay(c.Request.Context(), store, task, primaryKeys[task.ID]); poster != nil {
				items = append(items, item{ID: task.ID, Poster: poster})
			}
		}
	}
	common.ApiSuccess(c, gin.H{"items": items})
}

// Authentication and tenancy are checked from current users, in one batch.
func authorizedTaskVideoPosterOwners(ctx context.Context, db *gorm.DB, tasks []*model.Task, viewerID int, admin bool) (map[int64]bool, error) {
	ids := []int{viewerID}
	for _, task := range tasks {
		ids = append(ids, task.UserId)
	}
	var users []model.User
	if err := db.WithContext(ctx).Select("id", "role", "status", "tenant_id").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	byID := make(map[int]model.User, len(users))
	for _, user := range users {
		byID[user.Id] = user
	}
	viewer := byID[viewerID]
	allowed := make(map[int64]bool)
	if viewer.Id <= 0 || viewer.Status != common.UserStatusEnabled {
		return allowed, nil
	}
	platformAdmin := admin && viewer.Role >= common.RoleAdminUser && viewer.TenantID <= model.PlatformTenantID
	for _, task := range tasks {
		owner := byID[task.UserId]
		if owner.Id > 0 && owner.Status == common.UserStatusEnabled && owner.TenantID == task.TenantID &&
			(platformAdmin || task.UserId == viewerID && viewer.TenantID == task.TenantID) {
			allowed[task.ID] = true
		}
	}
	return allowed, nil
}

func taskVideoPosterArtifactKeys(task *model.Task) ([]string, error) {
	if task == nil || task.Status != model.TaskStatusSuccess || task.IsAssetHostedResult() || !task.ResultRetrievable() {
		return nil, nil
	}
	video := false
	for _, action := range constant.VideoTaskActions() {
		video = video || task.Action == action
	}
	if !video {
		return nil, nil
	}
	if legacyVideoAvailable(task) {
		return []string{"video"}, nil
	}
	artifacts, err := projectTaskArtifacts(task)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0)
	for _, artifact := range artifacts {
		if artifact.Type == "video" {
			keys = append(keys, artifact.Key)
		}
	}
	return keys, nil
}

func resolveTaskVideoPosterSource(ctx context.Context, task *model.Task, key string) (service.TaskVideoPosterSource, error) {
	usesStudio, err := service.TaskUsesStudioAssets(ctx, model.DB, task)
	if err != nil || usesStudio {
		return service.TaskVideoPosterSource{}, service.ErrTaskVideoPosterUnavailable
	}
	keys, err := taskVideoPosterArtifactKeys(task)
	if err != nil {
		return service.TaskVideoPosterSource{}, err
	}
	found := false
	for _, candidate := range keys {
		found = found || candidate == key
	}
	if !found {
		return service.TaskVideoPosterSource{}, service.ErrTaskVideoPosterUnavailable
	}
	descriptor := &relaychannel.TaskContentRequest{URL: task.GetResultURL(), Method: http.MethodGet, Credentialless: true}
	if taskHasPluginExecution(task) {
		adaptor, err := initTaskArtifactAdaptor(task)
		if err != nil {
			return service.TaskVideoPosterSource{}, err
		}
		provider, ok := adaptor.(relaychannel.TaskContentRequestProvider)
		if !ok {
			return service.TaskVideoPosterSource{}, service.ErrTaskVideoPosterUnavailable
		}
		descriptor, err = provider.BuildContentRequest(task, key, relaychannel.TaskArtifactClientRequest{Method: http.MethodGet})
		if err != nil {
			return service.TaskVideoPosterSource{}, err
		}
	}
	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		return service.TaskVideoPosterSource{}, err
	}
	return taskVideoPosterSourceFromDescriptor(task.TaskID, descriptor, channel)
}

// Provider headers may only reach the exact configured origin; the fetcher
// applies its own DNS/redirect and private-address checks before I/O.
func taskVideoPosterSourceFromDescriptor(taskID string, descriptor *relaychannel.TaskContentRequest, channel *model.Channel) (service.TaskVideoPosterSource, error) {
	if channel == nil || descriptor == nil || descriptor.Method != http.MethodGet || len(descriptor.Body) != 0 || isTaskMediaFallbackLoop(descriptor.URL, taskID) {
		return service.TaskVideoPosterSource{}, service.ErrTaskVideoPosterUnavailable
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.GetChannelBaseURL(channel.Type)
	}
	if descriptor.Credentialless && len(descriptor.Headers) != 0 {
		return service.TaskVideoPosterSource{}, service.ErrTaskVideoPosterUnavailable
	}
	headers := make(http.Header)
	if applyTaskMediaRequestHeaders(headers, descriptor.Headers) != nil {
		return service.TaskVideoPosterSource{}, service.ErrTaskVideoPosterUnavailable
	}
	if len(descriptor.Headers) > 0 {
		sourceURL, sourceErr := url.Parse(descriptor.URL)
		providerURL, providerErr := url.Parse(baseURL)
		if sourceErr != nil || providerErr != nil || !sameTaskMediaOrigin(sourceURL, providerURL) {
			return service.TaskVideoPosterSource{}, service.ErrTaskVideoPosterUnavailable
		}
	}
	return service.TaskVideoPosterSource{URL: descriptor.URL, Headers: descriptor.Headers, ProxyURL: channel.GetSetting().Proxy, ProviderURL: baseURL}, nil
}

type taskVideoPosterRuntime struct {
	mu       sync.Mutex
	workerID string
	store    *service.S3VideoAssetStore
	worker   *service.TaskVideoPosterWorker
}

func (runtime *taskVideoPosterRuntime) process(ctx context.Context) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	store, err := service.VideoStudioR2AssetStore(ctx)
	if err != nil {
		return service.ErrTaskVideoPosterUnavailable
	}
	if runtime.worker == nil || runtime.store != store {
		media, err := service.TaskVideoPosterProcessor(ctx)
		if err != nil {
			return service.ErrTaskVideoPosterUnavailable
		}
		tempDir := strings.TrimSpace(os.Getenv("VIDEO_STUDIO_TEMP_DIR"))
		if tempDir == "" {
			tempDir = os.TempDir()
		}
		worker, err := service.NewTaskVideoPosterWorker(model.DB, runtime.workerID, store, media, service.NewHTTPVideoArchiveFetcher(tempDir), resolveTaskVideoPosterSource, tempDir)
		if err != nil {
			return err
		}
		runtime.worker, runtime.store = worker, store
	}
	return runtime.worker.ProcessOnce(ctx)
}

func RegisterTaskVideoPosterBackgroundJobs(registry *service.BackgroundJobRegistry, workerID string) error {
	runtime := &taskVideoPosterRuntime{workerID: workerID}
	if err := registry.Register(service.BackgroundJob{Name: "task-video-posters", Interval: 2 * time.Second, RunOnStart: true, WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			if !service.TaskVideoPostersEnabled() {
				return nil
			}
			return runtime.process(ctx)
		},
	}); err != nil {
		return err
	}
	var cursor int64
	if err := registry.Register(service.BackgroundJob{Name: "task-video-poster-prepare", Interval: 10 * time.Second, RunOnStart: true, WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			if !service.TaskVideoPostersEnabled() {
				return nil
			}
			if _, err := service.TaskVideoPosterProcessor(ctx); err != nil {
				return service.ErrTaskVideoPosterUnavailable
			}
			if _, err := service.VideoStudioR2AssetStore(ctx); err != nil {
				return service.ErrTaskVideoPosterUnavailable
			}
			// Cycle through recent completions, including older IDs which finished
			// late. Historical pages use the authorized batch prepare endpoint.
			var tasks []*model.Task
			if err := model.DB.WithContext(ctx).Where("id > ? AND status = ? AND finish_time >= ? AND action IN ?", cursor, model.TaskStatusSuccess, time.Now().Add(-24*time.Hour).Unix(), constant.VideoTaskActions()).Order("id ASC").Limit(50).Find(&tasks).Error; err != nil {
				return err
			}
			if len(tasks) == 0 {
				cursor = 0
				return nil
			}
			var hosted []model.KKAIVideoGeneration
			ids := make([]int64, 0, len(tasks))
			userIDs := make([]int, 0, len(tasks))
			for _, task := range tasks {
				ids = append(ids, task.ID)
				userIDs = append(userIDs, task.UserId)
			}
			if err := model.DB.WithContext(ctx).Select("task_id").Where("task_id IN ?", ids).Find(&hosted).Error; err != nil {
				return err
			}
			hostedIDs := make(map[int64]bool, len(hosted))
			for _, generation := range hosted {
				hostedIDs[generation.TaskID] = true
			}
			var owners []model.User
			if err := model.DB.WithContext(ctx).Select("id", "status", "tenant_id").Where("id IN ?", userIDs).Find(&owners).Error; err != nil {
				return err
			}
			byOwner := make(map[int]model.User, len(owners))
			for _, owner := range owners {
				byOwner[owner.Id] = owner
			}
			for _, task := range tasks {
				cursor = task.ID
				owner := byOwner[task.UserId]
				if task.IsAssetHostedResult() || hostedIDs[task.ID] || owner.Id == 0 || owner.Status != common.UserStatusEnabled || owner.TenantID != task.TenantID {
					continue
				}
				keys, err := taskVideoPosterArtifactKeys(task)
				if err != nil {
					continue
				}
				if len(keys) > 0 {
					if snapshot, exists := task.PrivateData.VideoPosters[keys[0]]; exists &&
						snapshot.Version == model.TaskVideoPosterVersion(task, keys[0]) &&
						(snapshot.State != "unavailable" || snapshot.Attempt >= 1 || snapshot.UpdatedAt > time.Now().Add(-30*time.Minute).Unix()) {
						continue
					}
					if err := service.QueueTaskVideoPoster(ctx, model.DB, task, keys[0]); err != nil && !errors.Is(err, context.Canceled) {
						return err
					}
				}
			}
			return nil
		},
	}); err != nil {
		return err
	}
	var cleanupCursor int64
	return registry.Register(service.BackgroundJob{Name: "task-video-poster-cleanup", Interval: time.Minute, RunOnStart: false, WritesData: true, RequiresLeaderLease: true,
		Run: func(ctx context.Context) error {
			if !service.TaskVideoPostersEnabled() {
				return nil
			}
			store, err := service.VideoStudioR2AssetStore(ctx)
			if err != nil {
				return err
			}
			next, err := service.CleanupTaskVideoPosterObjects(ctx, model.DB, store, cleanupCursor, 500)
			if err == nil {
				cleanupCursor = next
			}
			return err
		},
	})
}
