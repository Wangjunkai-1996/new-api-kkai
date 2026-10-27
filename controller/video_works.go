package controller

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func getVideoWorks(c *gin.Context, adminView bool) {
	c.Header("Cache-Control", "private, no-store")
	query := model.VideoWorksQuery{SyncTaskQueryParams: model.SyncTaskQueryParams{
		Platform: constant.TaskPlatform(c.Query("platform")), TaskID: c.Query("task_id"),
		Status: c.Query("status"), Action: c.Query("action"), ChannelID: c.Query("channel_id"),
	}, Query: strings.TrimSpace(c.Query("q")), StatusGroup: c.DefaultQuery("status_group", "all")}
	validGroup := query.StatusGroup == "all" || query.StatusGroup == "success" || query.StatusGroup == "processing" || query.StatusGroup == "failed"
	valid := c.Query("media_type") == "video" && validGroup && utf8.RuneCountInString(query.Query) <= 191 &&
		!(query.Status != "" && c.Query("status_group") != "") && (adminView || query.ChannelID == "")
	page, errPage := strconv.Atoi(c.DefaultQuery("p", "1"))
	pageSize, errSize := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	valid = valid && errPage == nil && errSize == nil && page > 0 && pageSize > 0 && pageSize <= 100
	if valid && page > int(^uint(0)>>1)/pageSize {
		valid = false
	}
	for key, target := range map[string]*int64{"start_timestamp": &query.StartTimestamp, "end_timestamp": &query.EndTimestamp} {
		if raw := c.Query(key); raw != "" {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || value < 0 {
				valid = false
			}
			*target = value
		}
	}
	if query.ChannelID != "" {
		channelID, err := strconv.ParseInt(query.ChannelID, 10, 64)
		valid = valid && err == nil && channelID > 0
	}
	if query.StartTimestamp > 0 && query.EndTimestamp > 0 && query.StartTimestamp > query.EndTimestamp {
		valid = false
	}
	if !valid {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "invalid_video_works_query", "message": "Invalid video works filters"})
		return
	}
	userID := c.GetInt("id")
	role := common.RoleCommonUser
	if adminView {
		userID = 0
		role = c.GetInt("role")
	}
	tasks, counts, total, err := model.ListVideoWorks(c.Request.Context(), model.DB, userID, (page-1)*pageSize, pageSize, query)
	if err != nil {
		common.SysError("query video works failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "code": "video_works_query_failed", "message": "Failed to load video works"})
		return
	}
	outputs, mediaErr := service.LoadTaskVideoOutputs(c.Request.Context(), model.DB, tasks, c.GetInt("id"), adminView)
	// Media authorization already loaded owners in one batch, including names.
	items := tasksToDto(tasks, false, role)
	var store service.VideoAssetStore
	var storeErr error
	storeLoaded := false
	epoch, epochErr := common.CurrentCreditEpoch()
	for i, item := range items {
		if item.Billing == nil {
			item.Billing = &dto.TaskBilling{}
		}
		available := false
		if epochErr == nil && epoch != nil && tasks[i].ID > 0 && tasks[i].Quota >= 0 {
			if _, exists := epoch.LegacyMaxIDs["tasks"]; exists {
				quota, quotaErr := common.HistoricalCreditQuota("tasks", tasks[i].ID, tasks[i].Quota)
				available = quotaErr == nil && quota >= 0
			}
		}
		item.Billing.AmountAvailable = &available
		output := outputs[item.ID]
		if output == nil {
			// A batch error cannot establish that an ordinary task uses Studio.
			continue
		}
		if mediaErr != nil {
			item.Media = &dto.TaskVideoMedia{Source: "studio", State: "unavailable"}
			continue
		}
		item.Media = &output.Media
		if item.Media.State != "ready" {
			continue
		}
		if !storeLoaded {
			store, storeErr = videoStudioAssetStore(c)
			storeLoaded = true
		}
		signErr := storeErr
		if signErr == nil {
			signErr = output.SignPoster(c.Request.Context(), store)
		}
		if signErr != nil {
			item.Media.State = "unavailable"
			common.SysError("sign video work poster failed: " + signErr.Error())
		}
	}
	response := gin.H{"page": page, "page_size": pageSize, "total": total, "items": items, "status_counts": counts}
	if mediaErr != nil {
		common.SysError("load video work media failed: " + mediaErr.Error())
		response["media_warning"] = "media_temporarily_unavailable"
	}
	common.ApiSuccess(c, response)
}

// Dashboard artifact projection has the task's access boundary, which permits
// platform admins to inspect output media without weakening Studio asset APIs.
func writeHostedTaskArtifacts(c *gin.Context, task *model.Task) bool {
	isVideo := task.IsAssetHostedResult()
	for _, action := range constant.VideoTaskActions() {
		isVideo = isVideo || task.Action == action
	}
	if !isVideo {
		return false
	}
	outputs, err := service.LoadTaskVideoOutputs(c.Request.Context(), model.DB, []*model.Task{task}, c.GetInt("id"), c.GetInt("token_id") == 0 && c.GetInt("role") >= common.RoleAdminUser)
	if err != nil {
		common.SysError("load hosted task artifact failed: " + err.Error())
		writeTaskArtifactError(c, http.StatusInternalServerError, "artifact_internal_error", "Failed to load video media")
		return true
	}
	output := outputs[task.ID]
	if output == nil {
		return false
	}
	var store service.VideoAssetStore
	if output.Media.State == "ready" {
		store, err = videoStudioAssetStore(c)
	}
	var view *dto.TaskHostedVideo
	if err == nil {
		view, err = output.SignContent(c.Request.Context(), store)
	}
	if err != nil {
		common.SysError("sign hosted task artifact failed: " + err.Error())
		view = &dto.TaskHostedVideo{TaskRecordID: task.ID, State: "unavailable"}
	}
	c.Header("Cache-Control", "private, no-store")
	common.ApiSuccess(c, gin.H{"task_id": task.TaskID, "artifacts": []taskArtifactResponse{}, "hosted_video": view})
	return true
}
