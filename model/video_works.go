package model

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

type VideoWorksQuery struct {
	SyncTaskQueryParams
	Query       string
	StatusGroup string
}

type VideoWorksCounts struct {
	All        int64 `json:"all"`
	Success    int64 `json:"success"`
	Processing int64 `json:"processing"`
	Failed     int64 `json:"failed"`
	Unknown    int64 `json:"unknown"`
}

// videoWorksScope is shared by pagination and counts, including authorization.
func videoWorksScope(db *gorm.DB, userID int, params VideoWorksQuery) *gorm.DB {
	query := db.Model(&Task{}).Where("action IN ?", constant.VideoTaskActions())
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if params.ChannelID != "" {
		query = query.Where("channel_id = ?", params.ChannelID)
	}
	if params.Platform != "" {
		query = query.Where("platform = ?", params.Platform)
	}
	if params.Action != "" {
		query = query.Where("action = ?", params.Action)
	}
	if params.TaskID != "" {
		query = query.Where("task_id = ?", params.TaskID)
	}
	if params.StartTimestamp > 0 {
		query = query.Where("submit_time >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp > 0 {
		query = query.Where("submit_time <= ?", params.EndTimestamp)
	}
	if params.Query != "" {
		modelName := "CASE WHEN json_valid(properties) THEN COALESCE(NULLIF(json_extract(properties, '$.origin_model_name'), ''), json_extract(properties, '$.upstream_model_name'), '') ELSE '' END"
		switch {
		case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
			modelName = "COALESCE(NULLIF(properties->>'origin_model_name', ''), properties->>'upstream_model_name', '')"
		case common.UsingMainDatabase(common.DatabaseTypeMySQL):
			modelName = "COALESCE(NULLIF(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(properties, '$.origin_model_name')), 'null'), ''), NULLIF(JSON_UNQUOTE(JSON_EXTRACT(properties, '$.upstream_model_name')), 'null'), '')"
		}
		literal := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(params.Query))
		pattern := "%" + literal + "%"
		query = query.Where("(LOWER(task_id) LIKE ? ESCAPE '!' OR LOWER("+modelName+") LIKE ? ESCAPE '!')", pattern, pattern)
	}
	return query
}

// ListVideoWorks never projects artifacts or loads media. Unknown statuses stay
// visible in All and cannot accidentally be counted as active generation.
func ListVideoWorks(ctx context.Context, db *gorm.DB, userID, offset, limit int, params VideoWorksQuery) ([]*Task, VideoWorksCounts, int64, error) {
	var counts VideoWorksCounts
	var grouped []struct {
		Status string
		Count  int64
	}
	query := videoWorksScope(db.WithContext(ctx), userID, params)
	if err := query.Session(&gorm.Session{}).Select("status, COUNT(*) AS count").Group("status").Scan(&grouped).Error; err != nil {
		return nil, counts, 0, err
	}
	var total int64
	for _, row := range grouped {
		counts.All += row.Count
		switch row.Status {
		case TaskStatusSuccess:
			counts.Success += row.Count
		case TaskStatusFailure:
			counts.Failed += row.Count
		case string(TaskStatusNotStart), TaskStatusSubmitted, TaskStatusQueued, TaskStatusInProgress:
			counts.Processing += row.Count
		default:
			counts.Unknown += row.Count
		}
		if params.Status != "" && row.Status == params.Status {
			total += row.Count
		}
	}
	switch params.StatusGroup {
	case "success":
		query = query.Where("status = ?", TaskStatusSuccess)
		total = counts.Success
	case "failed":
		query = query.Where("status = ?", TaskStatusFailure)
		total = counts.Failed
	case "processing":
		query = query.Where("status IN ?", []string{string(TaskStatusNotStart), TaskStatusSubmitted, TaskStatusQueued, TaskStatusInProgress})
		total = counts.Processing
	default:
		if params.Status != "" {
			query = query.Where("status = ?", params.Status)
		} else {
			total = counts.All
		}
	}
	if userID > 0 {
		query = query.Omit("channel_id", "data")
	} else {
		query = query.Omit("data")
	}
	items := make([]*Task, 0)
	err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&items).Error
	return items, counts, total, err
}

// LoadVideoWorksPosterData fetches only current-page rows that have a cached
// poster. Task.Data is needed to validate the poster's source version, but is
// deliberately omitted from the regular list and never returned to clients.
func LoadVideoWorksPosterData(ctx context.Context, db *gorm.DB, tasks []*Task) (map[int64][]byte, error) {
	const maxDataBytes = 256 << 10
	loaded := make(map[int64][]byte)
	ids := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		if len(task.PrivateData.VideoPosters) > 0 && !task.IsAssetHostedResult() {
			ids = append(ids, task.ID)
		}
	}
	if len(ids) == 0 {
		return loaded, nil
	}
	sizeExpr := "COALESCE(LENGTH(data), 0)"
	switch db.Dialector.Name() {
	case "sqlite", "mysql":
	case "postgres":
		sizeExpr = "COALESCE(OCTET_LENGTH(data::text), 0)"
	default:
		return loaded, fmt.Errorf("unsupported video works database dialect: %s", db.Dialector.Name())
	}
	var rows []Task
	if err := db.WithContext(ctx).Select("id", "data").Where("id IN ? AND "+sizeExpr+" <= ?", ids, maxDataBytes).Find(&rows).Error; err != nil {
		return loaded, err
	}
	for _, row := range rows {
		loaded[row.ID] = row.Data
	}
	return loaded, nil
}
