package controller

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type tokenAPIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type tokenPageResponse struct {
	Items []tokenResponseItem `json:"items"`
}

type tokenResponseItem struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Key    string `json:"key"`
	Status int    `json:"status"`
}

type tokenKeyResponse struct {
	Key string `json:"key"`
}

type tokenUsageAPIResponse struct {
	Code    bool                       `json:"code"`
	Success bool                       `json:"success"`
	Message string                     `json:"message"`
	Data    map[string]json.RawMessage `json:"data"`
}

type sqliteColumnInfo struct {
	Name string `gorm:"column:name"`
	Type string `gorm:"column:type"`
}

type legacyToken struct {
	Id                 int    `gorm:"primaryKey"`
	UserId             int    `gorm:"index"`
	Key                string `gorm:"column:key;type:char(48);uniqueIndex"`
	Status             int    `gorm:"default:1"`
	Name               string `gorm:"index"`
	CreatedTime        int64  `gorm:"bigint"`
	AccessedTime       int64  `gorm:"bigint"`
	ExpiredTime        int64  `gorm:"bigint;default:-1"`
	RemainQuota        int    `gorm:"default:0"`
	UnlimitedQuota     bool
	ModelLimitsEnabled bool
	ModelLimits        string  `gorm:"type:text"`
	AllowIps           *string `gorm:"default:''"`
	UsedQuota          int     `gorm:"default:0"`
	Group              string  `gorm:"column:group;default:''"`
	CrossGroupRetry    bool
	DeletedAt          gorm.DeletedAt `gorm:"index"`
}

func (legacyToken) TableName() string {
	return "tokens"
}

func openTokenControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	model.DB = db
	model.LOG_DB = db

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func migrateTokenControllerTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()

	if err := db.AutoMigrate(&model.Token{}); err != nil {
		t.Fatalf("failed to migrate token table: %v", err)
	}
}

func setupTokenControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db := openTokenControllerTestDB(t)
	migrateTokenControllerTestDB(t, db)
	return db
}

func setupTokenUsageControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db := setupTokenControllerTestDB(t)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("failed to migrate user table: %v", err)
	}
	return db
}

func openTokenControllerExternalDB(t *testing.T, dialect string, dsn string) (*gorm.DB, *bool) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.RedisEnabled = false
	common.UsingSQLite = false
	common.UsingMySQL = dialect == "mysql"
	common.UsingPostgreSQL = dialect == "postgres"

	var (
		db  *gorm.DB
		err error
	)
	switch dialect {
	case "mysql":
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	case "postgres":
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	default:
		t.Fatalf("unsupported dialect %q", dialect)
	}
	if err != nil {
		t.Fatalf("failed to open %s db: %v", dialect, err)
	}

	model.DB = db
	model.LOG_DB = db

	if db.Migrator().HasTable("tokens") {
		t.Skipf("refusing to run %s migration compatibility test against external database because tokens table already exists", dialect)
	}

	managedTokensTable := new(bool)

	t.Cleanup(func() {
		if *managedTokensTable && db.Migrator().HasTable("tokens") {
			_ = db.Migrator().DropTable("tokens")
		}
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db, managedTokensTable
}

func seedToken(t *testing.T, db *gorm.DB, userID int, name string, rawKey string) *model.Token {
	t.Helper()

	token := &model.Token{
		UserId:         userID,
		Name:           name,
		Key:            rawKey,
		Status:         common.TokenStatusEnabled,
		CreatedTime:    1,
		AccessedTime:   1,
		ExpiredTime:    -1,
		RemainQuota:    100,
		UnlimitedQuota: true,
		Group:          "default",
	}
	if err := db.Create(token).Error; err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	return token
}

func seedTokenUsageUser(t *testing.T, db *gorm.DB, userID int, quota int, usedQuota int) {
	t.Helper()

	user := &model.User{
		Id:          userID,
		Username:    fmt.Sprintf("usage-user-%d", userID),
		Password:    "password123",
		DisplayName: fmt.Sprintf("Usage User %d", userID),
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Email:       fmt.Sprintf("usage-user-%d@example.com", userID),
		Quota:       quota,
		UsedQuota:   usedQuota,
		Group:       "default",
		AffCode:     fmt.Sprintf("usage-aff-%d", userID),
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create usage user: %v", err)
	}
}

func seedTokenUsageToken(t *testing.T, db *gorm.DB, token model.Token) *model.Token {
	t.Helper()

	if token.Status == 0 {
		token.Status = common.TokenStatusEnabled
	}
	if token.CreatedTime == 0 {
		token.CreatedTime = 1
	}
	if token.AccessedTime == 0 {
		token.AccessedTime = 1
	}
	if token.ExpiredTime == 0 {
		token.ExpiredTime = -1
	}
	if token.Group == "" {
		token.Group = "default"
	}
	if err := db.Create(&token).Error; err != nil {
		t.Fatalf("failed to create usage token: %v", err)
	}
	return &token
}

func newAuthenticatedContext(t *testing.T, method string, target string, body any, userID int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var requestBody *bytes.Reader
	if body != nil {
		payload, err := common.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		requestBody = bytes.NewReader(payload)
	} else {
		requestBody = bytes.NewReader(nil)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, requestBody)
	if body != nil {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	ctx.Set("id", userID)
	return ctx, recorder
}

func newTokenUsageContext(t *testing.T, authorization string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/usage/token/", nil)
	if authorization != "" {
		ctx.Request.Header.Set("Authorization", authorization)
	}
	return ctx, recorder
}

func decodeAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder) tokenAPIResponse {
	t.Helper()

	var response tokenAPIResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode api response: %v", err)
	}
	return response
}

func decodeTokenUsageAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder) tokenUsageAPIResponse {
	t.Helper()

	var response tokenUsageAPIResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode token usage response: %v", err)
	}
	return response
}

func requireTokenUsageBool(t *testing.T, data map[string]json.RawMessage, key string, want bool) {
	t.Helper()

	raw, ok := data[key]
	if !ok {
		t.Fatalf("expected token usage field %q to be present", key)
	}
	var got bool
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("failed to decode %s as bool: %v", key, err)
	}
	if got != want {
		t.Fatalf("expected %s to be %v, got %v", key, want, got)
	}
}

func requireTokenUsageInt(t *testing.T, data map[string]json.RawMessage, key string, want int) {
	t.Helper()

	raw, ok := data[key]
	if !ok {
		t.Fatalf("expected token usage field %q to be present", key)
	}
	var got int
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("failed to decode %s as int: %v", key, err)
	}
	if got != want {
		t.Fatalf("expected %s to be %d, got %d", key, want, got)
	}
}

func requireTokenUsageString(t *testing.T, data map[string]json.RawMessage, key string, want string) {
	t.Helper()

	raw, ok := data[key]
	if !ok {
		t.Fatalf("expected token usage field %q to be present", key)
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("failed to decode %s as string: %v", key, err)
	}
	if got != want {
		t.Fatalf("expected %s to be %q, got %q", key, want, got)
	}
}

func getSQLiteColumnType(t *testing.T, db *gorm.DB, tableName string, columnName string) string {
	t.Helper()

	var columns []sqliteColumnInfo
	if err := db.Raw("PRAGMA table_info(" + tableName + ")").Scan(&columns).Error; err != nil {
		t.Fatalf("failed to inspect %s schema: %v", tableName, err)
	}

	for _, column := range columns {
		if column.Name == columnName {
			return strings.ToLower(column.Type)
		}
	}

	t.Fatalf("column %s not found in %s schema", columnName, tableName)
	return ""
}

func getTokenKeyColumnType(t *testing.T, db *gorm.DB, dialect string) string {
	t.Helper()

	switch dialect {
	case "sqlite":
		return getSQLiteColumnType(t, db, "tokens", "key")
	case "mysql":
		var columnType string
		if err := db.Raw(`SELECT COLUMN_TYPE FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			"tokens", "key").Scan(&columnType).Error; err != nil {
			t.Fatalf("failed to inspect mysql token key column: %v", err)
		}
		return strings.ToLower(columnType)
	case "postgres":
		var dataType string
		var maxLength sql.NullInt64
		if err := db.Raw(`SELECT data_type, character_maximum_length
			FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
			"tokens", "key").Row().Scan(&dataType, &maxLength); err != nil {
			t.Fatalf("failed to inspect postgres token key column: %v", err)
		}
		switch strings.ToLower(dataType) {
		case "character varying":
			return fmt.Sprintf("varchar(%d)", maxLength.Int64)
		case "character":
			return fmt.Sprintf("char(%d)", maxLength.Int64)
		default:
			if maxLength.Valid {
				return fmt.Sprintf("%s(%d)", strings.ToLower(dataType), maxLength.Int64)
			}
			return strings.ToLower(dataType)
		}
	default:
		t.Fatalf("unsupported dialect %q", dialect)
		return ""
	}
}

func runTokenMigrationCompatibilityTest(t *testing.T, db *gorm.DB, dialect string, managedTokensTable *bool) {
	t.Helper()

	legacyKey := strings.Repeat("a", 48)
	longKey := strings.Repeat("b", 64)

	if err := db.AutoMigrate(&legacyToken{}); err != nil {
		t.Fatalf("failed to create legacy token schema: %v", err)
	}
	if managedTokensTable != nil {
		*managedTokensTable = true
	}
	if err := db.Create(&legacyToken{
		UserId:             7,
		Key:                legacyKey,
		Status:             common.TokenStatusEnabled,
		Name:               "legacy-token",
		CreatedTime:        1,
		AccessedTime:       1,
		ExpiredTime:        -1,
		RemainQuota:        100,
		UnlimitedQuota:     true,
		ModelLimitsEnabled: false,
		ModelLimits:        "",
		AllowIps:           common.GetPointer(""),
		UsedQuota:          0,
		Group:              "default",
		CrossGroupRetry:    false,
	}).Error; err != nil {
		t.Fatalf("failed to seed legacy token row: %v", err)
	}

	if got := getTokenKeyColumnType(t, db, dialect); got != "char(48)" {
		t.Fatalf("expected legacy key column type char(48), got %q", got)
	}

	migrateTokenControllerTestDB(t, db)

	if got := getTokenKeyColumnType(t, db, dialect); got != "varchar(128)" {
		t.Fatalf("expected migrated key column type varchar(128), got %q", got)
	}

	var migratedToken model.Token
	if err := db.First(&migratedToken, "name = ?", "legacy-token").Error; err != nil {
		t.Fatalf("failed to load migrated token row: %v", err)
	}
	if migratedToken.Key != legacyKey {
		t.Fatalf("expected migrated token key %q, got %q", legacyKey, migratedToken.Key)
	}
	if migratedToken.Name != "legacy-token" {
		t.Fatalf("expected migrated token name to be preserved, got %q", migratedToken.Name)
	}

	inserted := model.Token{
		UserId:             8,
		Name:               "long-token",
		Key:                longKey,
		Status:             common.TokenStatusEnabled,
		CreatedTime:        1,
		AccessedTime:       1,
		ExpiredTime:        -1,
		RemainQuota:        200,
		UnlimitedQuota:     true,
		ModelLimitsEnabled: false,
		ModelLimits:        "",
		AllowIps:           common.GetPointer(""),
		UsedQuota:          0,
		Group:              "default",
		CrossGroupRetry:    false,
	}
	if err := db.Create(&inserted).Error; err != nil {
		t.Fatalf("failed to insert long token after migration: %v", err)
	}

	var fetched model.Token
	if err := db.First(&fetched, "id = ?", inserted.Id).Error; err != nil {
		t.Fatalf("failed to fetch long token after migration: %v", err)
	}
	if fetched.Key != longKey {
		t.Fatalf("expected long token key %q, got %q", longKey, fetched.Key)
	}
}

func TestTokenAutoMigrateUsesVarchar128KeyColumn(t *testing.T) {
	db := setupTokenControllerTestDB(t)

	if got := getTokenKeyColumnType(t, db, "sqlite"); got != "varchar(128)" {
		t.Fatalf("expected key column type varchar(128), got %q", got)
	}
}

func TestTokenMigrationFromChar48ToVarchar128(t *testing.T) {
	db := openTokenControllerTestDB(t)
	runTokenMigrationCompatibilityTest(t, db, "sqlite", nil)
}

func TestTokenMigrationFromChar48ToVarchar128MySQL(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run mysql migration compatibility test")
	}

	db, managedTokensTable := openTokenControllerExternalDB(t, "mysql", dsn)
	runTokenMigrationCompatibilityTest(t, db, "mysql", managedTokensTable)
}

func TestTokenMigrationFromChar48ToVarchar128Postgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_DSN to run postgres migration compatibility test")
	}

	db, managedTokensTable := openTokenControllerExternalDB(t, "postgres", dsn)
	runTokenMigrationCompatibilityTest(t, db, "postgres", managedTokensTable)
}

func TestGetAllTokensMasksKeyInResponse(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	token := seedToken(t, db, 1, "list-token", "abcd1234efgh5678")
	seedToken(t, db, 2, "other-user-token", "zzzz1234yyyy5678")

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/token/?p=1&size=10", nil, 1)
	GetAllTokens(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var page tokenPageResponse
	if err := common.Unmarshal(response.Data, &page); err != nil {
		t.Fatalf("failed to decode token page response: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected exactly one token, got %d", len(page.Items))
	}
	if page.Items[0].Key != token.GetMaskedKey() {
		t.Fatalf("expected masked key %q, got %q", token.GetMaskedKey(), page.Items[0].Key)
	}
	if strings.Contains(recorder.Body.String(), token.Key) {
		t.Fatalf("list response leaked raw token key: %s", recorder.Body.String())
	}
}

func TestSearchTokensMasksKeyInResponse(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	token := seedToken(t, db, 1, "searchable-token", "ijkl1234mnop5678")

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/token/search?keyword=searchable-token&p=1&size=10", nil, 1)
	SearchTokens(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var page tokenPageResponse
	if err := common.Unmarshal(response.Data, &page); err != nil {
		t.Fatalf("failed to decode search response: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected exactly one search result, got %d", len(page.Items))
	}
	if page.Items[0].Key != token.GetMaskedKey() {
		t.Fatalf("expected masked search key %q, got %q", token.GetMaskedKey(), page.Items[0].Key)
	}
	if strings.Contains(recorder.Body.String(), token.Key) {
		t.Fatalf("search response leaked raw token key: %s", recorder.Body.String())
	}
}

func TestGetTokenMasksKeyInResponse(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	token := seedToken(t, db, 1, "detail-token", "qrst1234uvwx5678")

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/token/"+strconv.Itoa(token.Id), nil, 1)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(token.Id)}}
	GetToken(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var detail tokenResponseItem
	if err := common.Unmarshal(response.Data, &detail); err != nil {
		t.Fatalf("failed to decode token detail response: %v", err)
	}
	if detail.Key != token.GetMaskedKey() {
		t.Fatalf("expected masked detail key %q, got %q", token.GetMaskedKey(), detail.Key)
	}
	if strings.Contains(recorder.Body.String(), token.Key) {
		t.Fatalf("detail response leaked raw token key: %s", recorder.Body.String())
	}
}

func TestUpdateTokenMasksKeyInResponse(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	token := seedToken(t, db, 1, "editable-token", "yzab1234cdef5678")

	body := map[string]any{
		"id":                   token.Id,
		"name":                 "updated-token",
		"expired_time":         -1,
		"remain_quota":         100,
		"unlimited_quota":      true,
		"model_limits_enabled": false,
		"model_limits":         "",
		"group":                "default",
		"cross_group_retry":    false,
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/token/", body, 1)
	UpdateToken(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var detail tokenResponseItem
	if err := common.Unmarshal(response.Data, &detail); err != nil {
		t.Fatalf("failed to decode token update response: %v", err)
	}
	if detail.Key != token.GetMaskedKey() {
		t.Fatalf("expected masked update key %q, got %q", token.GetMaskedKey(), detail.Key)
	}
	if strings.Contains(recorder.Body.String(), token.Key) {
		t.Fatalf("update response leaked raw token key: %s", recorder.Body.String())
	}
}

func TestGetTokenKeyRequiresOwnershipAndReturnsFullKey(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	token := seedToken(t, db, 1, "owned-token", "owner1234token5678")

	authorizedCtx, authorizedRecorder := newAuthenticatedContext(t, http.MethodPost, "/api/token/"+strconv.Itoa(token.Id)+"/key", nil, 1)
	authorizedCtx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(token.Id)}}
	GetTokenKey(authorizedCtx)

	authorizedResponse := decodeAPIResponse(t, authorizedRecorder)
	if !authorizedResponse.Success {
		t.Fatalf("expected authorized key fetch to succeed, got message: %s", authorizedResponse.Message)
	}

	var keyData tokenKeyResponse
	if err := common.Unmarshal(authorizedResponse.Data, &keyData); err != nil {
		t.Fatalf("failed to decode token key response: %v", err)
	}
	if keyData.Key != token.GetFullKey() {
		t.Fatalf("expected full key %q, got %q", token.GetFullKey(), keyData.Key)
	}

	unauthorizedCtx, unauthorizedRecorder := newAuthenticatedContext(t, http.MethodPost, "/api/token/"+strconv.Itoa(token.Id)+"/key", nil, 2)
	unauthorizedCtx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(token.Id)}}
	GetTokenKey(unauthorizedCtx)

	unauthorizedResponse := decodeAPIResponse(t, unauthorizedRecorder)
	if unauthorizedResponse.Success {
		t.Fatalf("expected unauthorized key fetch to fail")
	}
	if strings.Contains(unauthorizedRecorder.Body.String(), token.Key) {
		t.Fatalf("unauthorized key response leaked raw token key: %s", unauthorizedRecorder.Body.String())
	}
}

func TestGetTokenUsageValidityAndBalanceVisibility(t *testing.T) {
	userID := 2210
	now := common.GetTimestamp()
	tests := []struct {
		name              string
		token             model.Token
		userQuota         int
		userUsedQuota     int
		wantValid         bool
		wantReason        string
		wantUserGranted   int
		wantUserUsed      int
		wantUserAvailable int
	}{
		{
			name: "active finite token",
			token: model.Token{
				Status:      common.TokenStatusEnabled,
				ExpiredTime: -1,
				RemainQuota: 40,
				UsedQuota:   10,
			},
			userQuota:         70,
			userUsedQuota:     30,
			wantValid:         true,
			wantReason:        "",
			wantUserGranted:   100,
			wantUserUsed:      30,
			wantUserAvailable: 70,
		},
		{
			name: "disabled token",
			token: model.Token{
				Status:      common.TokenStatusDisabled,
				ExpiredTime: -1,
				RemainQuota: 40,
				UsedQuota:   10,
			},
			userQuota:         70,
			userUsedQuota:     30,
			wantValid:         false,
			wantReason:        model.TokenUsageInvalidReasonDisabled,
			wantUserGranted:   0,
			wantUserUsed:      0,
			wantUserAvailable: 0,
		},
		{
			name: "expired token",
			token: model.Token{
				Status:      common.TokenStatusEnabled,
				ExpiredTime: now - 1,
				RemainQuota: 40,
				UsedQuota:   10,
			},
			userQuota:         70,
			userUsedQuota:     30,
			wantValid:         false,
			wantReason:        model.TokenUsageInvalidReasonExpired,
			wantUserGranted:   0,
			wantUserUsed:      0,
			wantUserAvailable: 0,
		},
		{
			name: "exhausted finite token",
			token: model.Token{
				Status:      common.TokenStatusEnabled,
				ExpiredTime: -1,
				RemainQuota: 0,
				UsedQuota:   25,
			},
			userQuota:         70,
			userUsedQuota:     30,
			wantValid:         false,
			wantReason:        model.TokenUsageInvalidReasonExhausted,
			wantUserGranted:   0,
			wantUserUsed:      0,
			wantUserAvailable: 0,
		},
		{
			name: "unlimited token",
			token: model.Token{
				Status:         common.TokenStatusEnabled,
				ExpiredTime:    -1,
				RemainQuota:    0,
				UsedQuota:      25,
				UnlimitedQuota: true,
			},
			userQuota:         5,
			userUsedQuota:     95,
			wantValid:         true,
			wantReason:        "",
			wantUserGranted:   100,
			wantUserUsed:      95,
			wantUserAvailable: 5,
		},
		{
			name: "user balance exhausted",
			token: model.Token{
				Status:      common.TokenStatusEnabled,
				ExpiredTime: -1,
				RemainQuota: 30,
				UsedQuota:   20,
			},
			userQuota:         0,
			userUsedQuota:     100,
			wantValid:         true,
			wantReason:        "",
			wantUserGranted:   100,
			wantUserUsed:      100,
			wantUserAvailable: 0,
		},
	}

	for idx, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTokenUsageControllerTestDB(t)
			seedTokenUsageUser(t, db, userID+idx, tt.userQuota, tt.userUsedQuota)
			tt.token.UserId = userID + idx
			tt.token.Name = tt.name
			tt.token.Key = fmt.Sprintf("usage-token-%d", idx)
			token := seedTokenUsageToken(t, db, tt.token)

			ctx, recorder := newTokenUsageContext(t, "Bearer sk-"+token.Key)
			GetTokenUsage(ctx)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", recorder.Code)
			}
			response := decodeTokenUsageAPIResponse(t, recorder)
			if !response.Code {
				t.Fatalf("expected token usage success, got message: %s", response.Message)
			}
			if response.Data == nil {
				t.Fatalf("expected token usage data")
			}

			requireTokenUsageBool(t, response.Data, "token_is_valid", tt.wantValid)
			requireTokenUsageString(t, response.Data, "token_invalid_reason", tt.wantReason)
			requireTokenUsageInt(t, response.Data, "user_total_granted", tt.wantUserGranted)
			requireTokenUsageInt(t, response.Data, "user_total_used", tt.wantUserUsed)
			requireTokenUsageInt(t, response.Data, "user_total_available", tt.wantUserAvailable)

			if _, ok := response.Data["user_id"]; ok {
				t.Fatalf("expected token usage response to omit user_id")
			}
		})
	}
}

func TestGetTokenUsageQuotaLookupFailureDoesNotExposeRawError(t *testing.T) {
	db := setupTokenUsageControllerTestDB(t)
	userID := 3301
	seedTokenUsageUser(t, db, userID, 70, 30)
	token := seedTokenUsageToken(t, db, model.Token{
		UserId:      userID,
		Name:        "quota failure token",
		Key:         "usage-quota-failure",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: 40,
		UsedQuota:   10,
	})
	if err := db.Migrator().DropTable(&model.User{}); err != nil {
		t.Fatalf("failed to drop users table: %v", err)
	}

	ctx, recorder := newTokenUsageContext(t, "Bearer "+token.Key)
	GetTokenUsage(ctx)

	response := decodeTokenUsageAPIResponse(t, recorder)
	if response.Code || response.Success {
		t.Fatalf("expected quota lookup failure response")
	}
	body := strings.ToLower(recorder.Body.String())
	for _, rawFragment := range []string{"no such table", "select", "users"} {
		if strings.Contains(body, rawFragment) {
			t.Fatalf("quota lookup failure leaked raw database detail %q in response: %s", rawFragment, recorder.Body.String())
		}
	}
	if response.Message == "" {
		t.Fatalf("expected generic quota lookup failure message")
	}
}
