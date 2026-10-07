package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/image_pricing_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageStudioDiscoveredModelUsesPricingDefaultSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	const modelName = "gpt-image-2.5-sunburst"
	const providerBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	const initialQuota = 1_000_000
	const expectedQuota = 335_000
	previousQuotaPerUnit := common.QuotaPerUnit
	previousPolicy := image_pricing_setting.JSON()
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		require.NoError(t, image_pricing_setting.UpdateByJSONString(previousPolicy))
	})
	common.QuotaPerUnit = 500_000
	policy := image_pricing_setting.DefaultConfig()
	policy.Enabled = true
	policy.Models[modelName] = policy.Models["gpt-image-2"]
	delete(policy.Models, "gpt-image-2")
	policyJSON, err := common.Marshal(policy)
	require.NoError(t, err)
	require.NoError(t, image_pricing_setting.UpdateByJSONString(string(policyJSON)))
	imageBytes, err := base64.StdEncoding.DecodeString(providerBase64)
	require.NoError(t, err)

	for _, test := range []struct {
		mode         string
		path         string
		upstreamPath string
	}{
		{service.ImageStudioModeGeneration, "/pg/images", "/v1/images/generations"},
		{service.ImageStudioModeEdit, "/pg/images/edits", "/v1/images/edits"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			db := setupImageStudioIntegrationState(t)
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", 407).Update("quota", initialQuota).Error)
			var channel model.Channel
			require.NoError(t, db.First(&channel).Error)
			require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channel.Id).Update("model", modelName).Error)
			channel.Models = modelName

			type providerRequest struct {
				Model string `json:"model"`
				Size  string `json:"size"`
				Path  string
			}
			observed := make(chan providerRequest, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				defer request.Body.Close()
				var decoded providerRequest
				if test.mode == service.ImageStudioModeEdit {
					if !assert.NoError(t, request.ParseMultipartForm(1<<20)) {
						return
					}
					defer request.MultipartForm.RemoveAll()
					decoded.Model = request.PostForm.Get("model")
					decoded.Size = request.PostForm.Get("size")
				} else if !assert.NoError(t, common.DecodeJson(request.Body, &decoded)) {
					return
				}
				decoded.Path = request.URL.Path
				observed <- decoded
				writer.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(writer, `{"created":1,"data":[{"b64_json":%q}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`, providerBase64)
			}))
			t.Cleanup(upstream.Close)
			channel.BaseURL = &upstream.URL
			require.NoError(t, db.Save(&channel).Error)
			model.InitChannelCache()
			var token model.Token
			require.NoError(t, db.First(&token).Error)
			profiles, err := service.ListEffectiveImageModelProfiles(context.Background(), db, 407, token.Id, "192.0.2.1")
			require.NoError(t, err)
			require.Len(t, profiles, 1)
			require.Equal(t, modelName, profiles[0].Model)

			store := &imageIntegrationAssetStore{objects: map[string][]byte{}}
			pipeline, err := service.NewImageAssetPipeline(db, store, service.NewHTTPImageArchiveFetcher(t.TempDir()), 1<<20, 100)
			require.NoError(t, err)
			engine := imageStudioIntegrationEngine(pipeline)
			submission := service.ImageStudioSubmissionRequest{
				TokenID: token.Id, Model: modelName, Prompt: "A lighthouse with default dimensions",
				Parameters: map[string]any{},
			}
			if test.mode == service.ImageStudioModeEdit {
				submission.References = imageStudioEditTestReferences([][]byte{imageBytes})
			}
			body, err := common.Marshal(submission)
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, test.path+"/quote", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var quote struct {
				Success bool                     `json:"success"`
				Data    service.ImageStudioQuote `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &quote))
			require.True(t, quote.Success)
			require.Equal(t, expectedQuota, quote.Data.Quota)
			require.NotEmpty(t, quote.Data.QuoteToken)

			submission.QuoteToken = quote.Data.QuoteToken
			body, err = common.Marshal(submission)
			require.NoError(t, err)
			contentType := "application/json"
			if test.mode == service.ImageStudioModeEdit {
				body, contentType = imageStudioEditMultipartBody(t, body, [][]byte{imageBytes}, false)
			}
			request = httptest.NewRequest(http.MethodPost, test.path, bytes.NewReader(body))
			request.Header.Set("Content-Type", contentType)
			request.Header.Set("Idempotency-Key", "default-size-"+test.mode)
			response = httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
			select {
			case providerCall := <-observed:
				assert.Equal(t, providerRequest{Model: modelName, Size: "1024x1024", Path: test.upstreamPath}, providerCall)
			default:
				t.Fatal("submission did not reach the provider")
			}
			var generation model.KKAIImageGeneration
			require.NoError(t, db.First(&generation).Error)
			assert.Equal(t, model.ImageGenerationStatusSucceeded, generation.Status)
			assert.Equal(t, model.ImageGenerationBillingStateSettled, generation.BillingState)
			assert.Equal(t, expectedQuota, generation.FinalQuota)
			var user model.User
			require.NoError(t, db.First(&user, 407).Error)
			assert.EqualValues(t, initialQuota-expectedQuota, user.Quota)
		})
	}
}
