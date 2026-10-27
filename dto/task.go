package dto

import (
	"encoding/json"
)

type TaskError struct {
	// NoRetry prevents duplicate upstream work after an accepted response.
	NoRetry            bool   `json:"-"`
	Code               string `json:"code"`
	Message            string `json:"message"`
	Data               any    `json:"data"`
	StatusCode         int    `json:"-"`
	UpstreamStatusCode int    `json:"-"`
	UpstreamErrorCode  string `json:"-"`
	PolicyEvidence     string `json:"-"`
	LocalError         bool   `json:"-"`
	Error              error  `json:"-"`
}

type TaskData interface {
	SunoDataResponse | []SunoDataResponse | string | any
}

const TaskSuccessCode = "success"

type TaskResponse[T TaskData] struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

func (t *TaskResponse[T]) IsSuccess() bool {
	return t.Code == TaskSuccessCode
}

type TaskDto struct {
	ID                   int64           `json:"id"`
	CreatedAt            int64           `json:"created_at"`
	UpdatedAt            int64           `json:"updated_at"`
	TaskID               string          `json:"task_id"`
	Platform             string          `json:"platform"`
	UserId               int             `json:"user_id"`
	Group                string          `json:"group"`
	ChannelId            int             `json:"channel_id"`
	Quota                int             `json:"quota"`
	Billing              *TaskBilling    `json:"billing,omitempty"`
	Media                *TaskVideoMedia `json:"media,omitempty"`
	Failure              *TaskFailure   `json:"failure,omitempty"`
	Poster               *TaskVideoPoster `json:"poster,omitempty"`
	Action               string          `json:"action"`
	Status               string          `json:"status"`
	FailReason           string          `json:"fail_reason"`
	ResultURL            string          `json:"result_url,omitempty"`
	LegacyVideoAvailable bool            `json:"legacy_video_available,omitempty"`
	ResultDiscarded      bool            `json:"result_discarded,omitempty"`
	SubmitTime           int64           `json:"submit_time"`
	StartTime            int64           `json:"start_time"`
	FinishTime           int64           `json:"finish_time"`
	Progress             string          `json:"progress"`
	Properties           any             `json:"properties"`
	Username             string          `json:"username,omitempty"`
	Data                 json.RawMessage `json:"data"`
	AdminInfo            *TaskAdminInfo  `json:"admin_info,omitempty"`
	RootInfo             *TaskRootInfo   `json:"root_info,omitempty"`
}

type TaskBilling struct {
	AmountAvailable          *bool              `json:"amount_available,omitempty"`
	State                    string             `json:"state,omitempty"`
	BillingSource            string             `json:"billing_source,omitempty"`
	PricingEpoch             string             `json:"pricing_epoch,omitempty"`
	ModelPrice               *float64           `json:"model_price,omitempty"`
	ModelRatio               *float64           `json:"model_ratio,omitempty"`
	GroupRatio               *float64           `json:"group_ratio,omitempty"`
	UserGroupRatio           *float64           `json:"user_group_ratio,omitempty"`
	BillingUnit              string             `json:"billing_unit,omitempty"`
	BillingMode              string             `json:"billing_mode,omitempty"`
	Seconds                  *float64           `json:"seconds,omitempty"`
	Duration                 *float64           `json:"duration,omitempty"`
	Size                     *float64           `json:"size,omitempty"`
	Resolution               *float64           `json:"resolution,omitempty"`
	ImageCount               *float64           `json:"image_count,omitempty"`
	ExprB64                  string             `json:"expr_b64,omitempty"`
	MatchedTier              string             `json:"matched_tier,omitempty"`
	UsageFacts               map[string]float64 `json:"usage_facts,omitempty"`
	MembershipTierName       string             `json:"membership_tier_name,omitempty"`
	MembershipTierStyle      string             `json:"membership_tier_style,omitempty"`
	MembershipDiscount       *float64           `json:"membership_discount,omitempty"`
	MembershipWalletRatio    *float64           `json:"membership_wallet_ratio,omitempty"`
	MembershipCandidateRatio *float64           `json:"membership_candidate_ratio,omitempty"`
	MembershipSavings        *float64           `json:"membership_savings,omitempty"`
	MembershipWalletFallback *bool              `json:"membership_wallet_fallback,omitempty"`
}

type TaskVideoMedia struct {
	Source          string  `json:"source"`
	State           string  `json:"state"`
	PosterURL       string  `json:"poster_url,omitempty"`
	PosterExpiresAt int64   `json:"poster_expires_at,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

type TaskVideoPoster struct {
	State       string `json:"state"`
	URL         string `json:"url,omitempty"`
	ExpiresAt   int64  `json:"expires_at,omitempty"`
	ArtifactKey string `json:"artifact_key,omitempty"`
	FailureCode string `json:"failure_code,omitempty"`
}

type TaskHostedVideo struct {
	TaskRecordID    int64   `json:"task_record_id"`
	State           string  `json:"state"`
	ContentURL      string  `json:"content_url,omitempty"`
	DownloadURL     string  `json:"download_url,omitempty"`
	ExpiresAt       int64   `json:"expires_at,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

type TaskPluginInfo struct {
	Key     string                `json:"key"`
	Name    string                `json:"name"`
	Version string                `json:"version,omitempty"`
	Author  *TaskPluginAuthorInfo `json:"author,omitempty"`
}

type TaskPluginAuthorInfo struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}
type TaskPluginRuntimeInfo struct {
	Key        string `json:"key"`
	Version    string `json:"version"`
	APIVersion int    `json:"api_version"`
	Generation uint64 `json:"generation"`
}
type TaskAdminInfo struct {
	RequestID   string          `json:"request_id,omitempty"`
	RequestPath string          `json:"request_path,omitempty"`
	TaskPlugin  *TaskPluginInfo `json:"task_plugin,omitempty"`
}
type TaskRootInfo struct {
	TaskPlugin     *TaskPluginRuntimeInfo `json:"task_plugin,omitempty"`
	UpstreamTaskID string                 `json:"upstream_task_id,omitempty"`
	NodeName       string                 `json:"node_name,omitempty"`
}

type FetchReq struct {
	IDs []string `json:"ids"`
}
