package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPrepareImageStudioRequestRewritesOnlyValidatedRelayFields(t *testing.T) {
	db, token := newImageStudioRelayTestDB(t)
	body, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: "gpt-image-1", Prompt: "draw a lighthouse",
		Parameters: map[string]any{"count": 2},
	})
	require.NoError(t, err)
	ctx, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images/quote", body)

	PrepareImageStudioRequest(ctx)

	require.False(t, ctx.IsAborted())
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "/v1/images/generations", ctx.Request.URL.Path)
	assert.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyIsPlayground))
	assert.Equal(t, service.ImageStudioTokenGroup, common.GetContextKeyString(ctx, constant.ContextKeyUsingGroup))
	assert.Equal(t, token.Id, ctx.GetInt("token_id"))
	rewritten, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(rewritten, &payload))
	assert.Equal(t, "gpt-image-1", payload["model"])
	assert.Equal(t, "draw a lighthouse", payload["prompt"])
	assert.Equal(t, float64(2), payload["n"])
	assert.Equal(t, false, payload["stream"])
	assert.NotContains(t, payload, "token_id")
	assert.NotContains(t, payload, "parameters")
	assert.NotContains(t, payload, "extra_fields")
	assert.NotContains(t, payload, "response_format")
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeAzure)
	assert.ErrorIs(t, service.ValidateSelectedImageStudioChannel(ctx), service.ErrNoChannelSupportsImageOutputCount)

	var reservations int64
	require.NoError(t, db.Model(&model.KKAIIdempotencyKey{}).Count(&reservations).Error)
	assert.Zero(t, reservations)
}

func TestPrepareImageStudioEditQuoteBindsRouteModeAndReferences(t *testing.T) {
	_, token := newImageStudioRelayTestDB(t)
	references := []service.ImageStudioReferenceMetadata{
		{SHA256: strings.Repeat("a", 64), SizeBytes: 1234},
		{SHA256: strings.Repeat("b", 64), SizeBytes: 5678},
	}
	body, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: service.ImageStudioEditModel, Prompt: "edit a lighthouse",
		References: references,
	})
	require.NoError(t, err)
	ctx, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images/edits/quote", body)

	PrepareImageStudioRequest(ctx)

	require.False(t, ctx.IsAborted(), recorder.Body.String())
	assert.Equal(t, "/v1/images/edits", ctx.Request.URL.Path)
	normalized, ok := imageStudioNormalizedSubmission(ctx)
	require.True(t, ok)
	assert.Equal(t, service.ImageStudioModeEdit, normalized.Mode)
	assert.Equal(t, references, normalized.References)
	rewritten, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(rewritten, &payload))
	assert.Equal(t, service.ImageStudioEditModel, payload["model"])
	assert.NotContains(t, payload, "references")
}

func TestPrepareImageStudioEditSubmitValidatesAndRebuildsOrderedMultipart(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("VIDEO_STUDIO_TEMP_DIR", tempDir)
	_, token := newImageStudioRelayTestDB(t)
	imageBytes := [][]byte{
		imageStudioEditTestPNG(t, color.RGBA{R: 255, A: 255}),
		imageStudioEditTestPNG(t, color.RGBA{B: 255, A: 255}),
	}
	references := imageStudioEditTestReferences(imageBytes)
	requestJSON, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: service.ImageStudioEditModel, Prompt: "edit a lighthouse",
		QuoteToken: "quote-token",
		References: references,
	})
	require.NoError(t, err)
	body, contentType := imageStudioEditMultipartBody(t, requestJSON, imageBytes, false)
	ctx, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images/edits", body)
	ctx.Request.Header.Set("Content-Type", contentType)
	ctx.Request.Header.Set("Idempotency-Key", "edit-submit-rebuild")

	PrepareImageStudioRequest(ctx)

	require.False(t, ctx.IsAborted(), recorder.Body.String())
	assert.Equal(t, "/v1/images/edits", ctx.Request.URL.Path)
	normalized, ok := imageStudioNormalizedSubmission(ctx)
	require.True(t, ok)
	assert.Equal(t, references, normalized.References)

	rewritten, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	replayed := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(rewritten))
	replayed.Header.Set("Content-Type", ctx.Request.Header.Get("Content-Type"))
	require.NoError(t, replayed.ParseMultipartForm(1<<20))
	assert.Equal(t, service.ImageStudioEditModel, replayed.PostForm.Get("model"))
	assert.Equal(t, "edit a lighthouse", replayed.PostForm.Get("prompt"))
	assert.Equal(t, "1", replayed.PostForm.Get("n"))
	assert.Equal(t, "false", replayed.PostForm.Get("stream"))
	require.Len(t, replayed.MultipartForm.File["image"], len(imageBytes))
	for index, fileHeader := range replayed.MultipartForm.File["image"] {
		file, err := fileHeader.Open()
		require.NoError(t, err)
		gotImage, err := io.ReadAll(file)
		require.NoError(t, file.Close())
		require.NoError(t, err)
		assert.Equal(t, imageBytes[index], gotImage)
	}
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestPrepareImageStudioEditSubmitRejectsExtraMultipartFields(t *testing.T) {
	_, token := newImageStudioRelayTestDB(t)
	imageBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	require.NoError(t, err)
	requestJSON, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: service.ImageStudioEditModel, Prompt: "edit",
		QuoteToken: "quote-token",
		References: imageStudioEditTestReferences([][]byte{imageBytes}),
	})
	require.NoError(t, err)
	body, contentType := imageStudioEditMultipartBody(t, requestJSON, [][]byte{imageBytes}, true)
	ctx, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images/edits", body)
	ctx.Request.Header.Set("Content-Type", contentType)
	ctx.Request.Header.Set("Idempotency-Key", "edit-submit-extra-field")

	PrepareImageStudioRequest(ctx)

	assert.True(t, ctx.IsAborted())
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "invalid_image_studio_request")
}

func TestPrepareImageStudioEditSubmitRejectsInvalidMultipartCardinalityAndAliases(t *testing.T) {
	_, token := newImageStudioRelayTestDB(t)
	imageBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	require.NoError(t, err)
	requestJSON, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: service.ImageStudioEditModel, Prompt: "edit",
		QuoteToken: "quote-token",
		References: imageStudioEditTestReferences([][]byte{imageBytes}),
	})
	require.NoError(t, err)

	tests := []struct {
		name  string
		write func(*multipart.Writer)
	}{
		{
			name: "duplicate request",
			write: func(writer *multipart.Writer) {
				require.NoError(t, writer.WriteField("request", string(requestJSON)))
				require.NoError(t, writer.WriteField("request", string(requestJSON)))
				writeImageStudioEditTestImage(t, writer, "image", imageBytes)
			},
		},
		{
			name: "metadata and file count differ",
			write: func(writer *multipart.Writer) {
				require.NoError(t, writer.WriteField("request", string(requestJSON)))
				writeImageStudioEditTestImage(t, writer, "image", imageBytes)
				writeImageStudioEditTestImage(t, writer, "image", imageBytes)
			},
		},
		{
			name: "image array alias",
			write: func(writer *multipart.Writer) {
				require.NoError(t, writer.WriteField("request", string(requestJSON)))
				writeImageStudioEditTestImage(t, writer, "image[]", imageBytes)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			test.write(writer)
			require.NoError(t, writer.Close())
			ctx, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images/edits", body.Bytes())
			ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
			ctx.Request.Header.Set("Idempotency-Key", "edit-submit-cardinality")

			PrepareImageStudioRequest(ctx)

			assert.True(t, ctx.IsAborted())
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "invalid_image_studio_request")
		})
	}
}

func TestPrepareImageStudioEditSubmitRejectsMalformedMultipart(t *testing.T) {
	ctx, recorder := newImageStudioRelayContext(
		http.MethodPost, "/pg/images/edits", []byte("not-a-multipart-body"),
	)
	ctx.Request.Header.Set("Content-Type", "multipart/form-data; boundary=missing")

	PrepareImageStudioRequest(ctx)

	assert.True(t, ctx.IsAborted())
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "invalid_image_studio_request")
}

func TestPrepareImageStudioSubmitRequiresIdempotencyBeforeRelay(t *testing.T) {
	_, token := newImageStudioRelayTestDB(t)
	body, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: "gpt-image-1", Prompt: "draw a lighthouse",
		QuoteToken: "quote-token",
	})
	require.NoError(t, err)
	ctx, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images", body)

	PrepareImageStudioRequest(ctx)

	assert.True(t, ctx.IsAborted())
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "idempotency_key_required")
}

func TestPrepareImageStudioRequestReleasesUnboundReservationAfterClientCancellation(t *testing.T) {
	db, token := newImageStudioRelayTestDB(t)
	body, err := common.Marshal(service.ImageStudioSubmissionRequest{
		TokenID: token.Id, Model: "gpt-image-1", Prompt: "draw a lighthouse",
		QuoteToken: "quote-token",
	})
	require.NoError(t, err)
	requestContext, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/pg/images", bytes.NewReader(body)).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "disconnect-before-binding")
	recorder := httptest.NewRecorder()
	engine := gin.New()
	engine.POST("/pg/images", func(c *gin.Context) {
		c.Set("id", 7)
		c.Set("user_group", "default")
	}, PrepareImageStudioRequest, func(c *gin.Context) {
		cancel()
		c.Status(http.StatusNoContent)
	})

	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNoContent, recorder.Code)
	var reservations int64
	require.NoError(t, db.Model(&model.KKAIIdempotencyKey{}).Count(&reservations).Error)
	require.Zero(t, reservations)
}

func TestImageStudioErrorStatusPreservesClientAndConcurrencyFailures(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{service.ErrImageModelProfileConflict, http.StatusConflict, "image_studio_conflict"},
		{imagepricing.ErrUnsupportedSize, http.StatusBadRequest, "invalid_image_size"},
		{service.ErrImageArchiveTooLarge, http.StatusRequestEntityTooLarge, "image_asset_too_large"},
		{service.ErrImageArchiveMIMERejected, http.StatusBadRequest, "invalid_image_asset"},
		{service.ErrImageTemporaryStorageUnavailable, http.StatusServiceUnavailable, "image_temporary_storage_unavailable"},
		{service.ErrNoChannelSupportsImageOutputCount, http.StatusServiceUnavailable, "image_output_count_unavailable"},
	}
	for _, test := range tests {
		status, code := imageStudioErrorStatus(test.err)
		assert.Equal(t, test.status, status)
		assert.Equal(t, test.code, code)
	}
}

func TestImageStudioQuoteAndSubmitRejectOutputIncompatibleSelectedChannel(t *testing.T) {
	tests := []struct {
		name    string
		handler gin.HandlerFunc
	}{
		{"quote", QuoteImageStudioGeneration},
		{"submit", SubmitImageStudioGeneration},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images", nil)
			ctx.Set(imageStudioNormalizedSubmissionContextKey, &service.NormalizedImageStudioSubmission{
				RequestedCount: 2,
			})
			service.SetImageStudioRequestedOutputCount(ctx, 2)
			common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeAzure)

			test.handler(ctx)

			assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "image_output_count_unavailable")
		})
	}
}

func TestImageStudioSubmissionCapacityEnforcesGlobalAndPerUserBounds(t *testing.T) {
	resetImageStudioSubmissionCapacity(t)
	require.True(t, imageStudioCapacity.acquire(7, 2, 1))
	require.False(t, imageStudioCapacity.acquire(7, 2, 1))
	require.True(t, imageStudioCapacity.acquire(8, 2, 1))
	require.False(t, imageStudioCapacity.acquire(9, 2, 1))

	imageStudioCapacity.release(7)
	require.True(t, imageStudioCapacity.acquire(9, 2, 1))
	imageStudioCapacity.release(8)
	imageStudioCapacity.release(9)
	imageStudioCapacity.mu.Lock()
	require.Zero(t, imageStudioCapacity.total)
	require.Empty(t, imageStudioCapacity.byUser)
	imageStudioCapacity.mu.Unlock()

	status, code := imageStudioErrorStatus(service.ErrImageStudioCapacityExceeded)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "image_studio_busy", code)
}

func TestImageStudioIdempotentReplayStatusContract(t *testing.T) {
	db, token := newImageStudioRelayTestDB(t)
	now := time.Now().Unix()
	tests := []struct {
		status       string
		responseCode int
		retryAfter   string
		viewStatus   string
	}{
		{model.ImageGenerationStatusSubmitting, http.StatusAccepted, "2", model.ImageGenerationStatusSubmitting},
		{model.ImageGenerationStatusRecovering, http.StatusAccepted, "2", model.ImageGenerationStatusSubmitting},
		{model.ImageGenerationStatusSucceeded, http.StatusOK, "", model.ImageGenerationStatusSucceeded},
	}
	for index, test := range tests {
		billingState := model.ImageGenerationBillingStatePending
		if test.status == model.ImageGenerationStatusSucceeded {
			billingState = model.ImageGenerationBillingStateSettled
		}
		generation := model.KKAIImageGeneration{
			UserID: 7, TokenID: token.Id, ModelProfileID: 1, SpecificationVersion: 1,
			Model: "gpt-image-1", Prompt: "idempotent replay", Parameters: `{}`,
			RequestHash: fmt.Sprintf("%064d", index+1), RequestID: fmt.Sprintf("replay-%d", index),
			Status: test.status, RequestedCount: 1, BillingState: billingState,
			HeartbeatAt: now, StartedAt: now, CreatedAt: now, UpdatedAt: now,
		}
		require.NoError(t, db.Create(&generation).Error)
		ctx, recorder := newImageStudioRelayContext(http.MethodPost, "/pg/images", nil)

		respondImageStudioIdempotentReplay(ctx, strconv.FormatInt(generation.ID, 10))

		require.Equal(t, test.responseCode, recorder.Code)
		assert.Equal(t, test.retryAfter, recorder.Header().Get("Retry-After"))
		assert.Contains(t, recorder.Body.String(), `"status":"`+test.viewStatus+`"`)
	}
}

func resetImageStudioSubmissionCapacity(t *testing.T) {
	t.Helper()
	reset := func() {
		imageStudioCapacity.mu.Lock()
		imageStudioCapacity.total = 0
		clear(imageStudioCapacity.byUser)
		imageStudioCapacity.mu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func newImageStudioRelayTestDB(t *testing.T) (*gorm.DB, model.Token) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	previousRedisEnabled := common.RedisEnabled
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	previousSpecialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.MarshalJSONString()
	common.RedisEnabled = false
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","图片工作室":"图片工作室"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"图片工作室":1}`))
	require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(`{}`)))

	dsn := fmt.Sprintf("file:image-relay-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{},
		&model.KKAIImageModelProfile{}, &model.KKAIImageSample{}, &model.KKAIImageGeneration{},
		&model.KKAIImageAsset{}, &model.KKAIIdempotencyKey{},
	))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedisEnabled
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
		require.NoError(t, ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.UnmarshalJSON([]byte(previousSpecialGroups)))
	})
	require.NoError(t, db.Create(&model.User{
		Id: 7, Username: "image-relay-user", Password: "password", DisplayName: "Image User",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default",
	}).Error)
	token := model.Token{
		UserId: 7, Key: fmt.Sprintf("image-relay-key-%d", time.Now().UnixNano()),
		Status: common.TokenStatusEnabled, Name: service.ImageStudioTokenGroup,
		CreatedTime: time.Now().Unix(), AccessedTime: time.Now().Unix(), ExpiredTime: -1,
		UnlimitedQuota: true, Group: service.ImageStudioTokenGroup,
	}
	require.NoError(t, db.Create(&token).Error)
	channel := model.Channel{
		Type: constant.ChannelTypeOpenAI, Key: "test-key", Status: common.ChannelStatusEnabled,
		Name: "image channel", Models: "gpt-image-1,gpt-image-2", Group: service.ImageStudioTokenGroup,
	}
	require.NoError(t, db.Create(&channel).Error)
	priority := int64(0)
	for _, modelName := range []string{"gpt-image-1", service.ImageStudioEditModel} {
		require.NoError(t, db.Create(&model.Ability{
			Group: service.ImageStudioTokenGroup, Model: modelName, ChannelId: channel.Id,
			Enabled: true, Priority: &priority,
		}).Error)
	}
	minimum := 1
	maximum := 4
	specification, err := common.Marshal(service.ImageModelSpec{Version: 1, MaxReferenceImages: 2, Parameters: []service.ImageParameterSpec{
		{Key: "count", Label: "Count", Control: service.ImageControlInteger, RequestKey: "n", Min: &minimum, Max: &maximum},
	}})
	require.NoError(t, err)
	for _, modelName := range []string{"gpt-image-1", service.ImageStudioEditModel} {
		require.NoError(t, db.Create(&model.KKAIImageModelProfile{
			Model: modelName, DisplayName: "Image Model", Description: "test",
			SpecificationVersion: 1, Specification: string(specification), DefaultParameters: `{"count":1}`,
			Enabled: true, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
		}).Error)
	}
	return db, token
}

func imageStudioEditMultipartBody(
	t *testing.T,
	requestJSON []byte,
	images [][]byte,
	withExtraField bool,
) ([]byte, string) {
	mimeTypes := make([]string, len(images))
	for index := range mimeTypes {
		mimeTypes[index] = "image/png"
	}
	return imageStudioEditMultipartBodyWithMIMEs(t, requestJSON, images, mimeTypes, withExtraField)
}

func imageStudioEditMultipartBodyWithMIMEs(
	t *testing.T,
	requestJSON []byte,
	images [][]byte,
	mimeTypes []string,
	withExtraField ...bool,
) ([]byte, string) {
	t.Helper()
	require.Len(t, mimeTypes, len(images))
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("request", string(requestJSON)))
	if len(withExtraField) > 0 && withExtraField[0] {
		require.NoError(t, writer.WriteField("unexpected", "rejected"))
	}
	for index, imageBytes := range images {
		writeImageStudioEditTestImageWithMIME(t, writer, "image", imageBytes, mimeTypes[index])
	}
	require.NoError(t, writer.Close())
	return body.Bytes(), writer.FormDataContentType()
}

func imageStudioEditTestReferences(images [][]byte) []service.ImageStudioReferenceMetadata {
	references := make([]service.ImageStudioReferenceMetadata, len(images))
	for index, imageBytes := range images {
		digest := sha256.Sum256(imageBytes)
		references[index] = service.ImageStudioReferenceMetadata{
			SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len(imageBytes)),
		}
	}
	return references
}

func imageStudioEditTestPNG(t *testing.T, fill color.Color) []byte {
	return imageStudioEditTestPNGSize(t, 1, 1, fill)
}

func imageStudioEditTestPNGSize(t *testing.T, width int, height int, fill color.Color) []byte {
	t.Helper()
	imageValue := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			imageValue.Set(x, y, fill)
		}
	}
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, imageValue))
	return encoded.Bytes()
}

func writeImageStudioEditTestImage(t *testing.T, writer *multipart.Writer, fieldName string, imageBytes []byte) {
	writeImageStudioEditTestImageWithMIME(t, writer, fieldName, imageBytes, "image/png")
}

func writeImageStudioEditTestImageWithMIME(
	t *testing.T,
	writer *multipart.Writer,
	fieldName string,
	imageBytes []byte,
	mimeType string,
) {
	t.Helper()
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename="reference.png"`, fieldName))
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write(imageBytes)
	require.NoError(t, err)
}

func newImageStudioRelayContext(method string, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 7)
	ctx.Set("user_group", "default")
	return ctx, recorder
}
