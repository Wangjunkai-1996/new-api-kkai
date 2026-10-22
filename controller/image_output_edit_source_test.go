package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/image_studio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type imageOutputControllerStore struct {
	imageIntegrationAssetStore
	object service.ImageAssetObject
	err    error
}

func (store *imageOutputControllerStore) Get(context.Context, string) (service.ImageAssetObject, error) {
	return store.object, store.err
}

func TestImageOutputEditSourceHTTPContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:edit-source-controller-%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.KKAIImageGeneration{}, &model.KKAIImageAsset{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	settings := image_studio_setting.Get()
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"image_studio.max_reference_bytes": "256"}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"image_studio.max_reference_bytes": fmt.Sprint(settings.MaxReferenceBytes)}))
	})
	payload := imageStudioEditTestPNG(t, color.RGBA{R: 255, A: 255})
	digest := sha256.Sum256(payload)
	generation := model.KKAIImageGeneration{
		UserID: 7, RequestID: "edit-source-controller", Status: model.ImageGenerationStatusSucceeded,
		BillingState: model.ImageGenerationBillingStateSettled,
	}
	require.NoError(t, db.Create(&generation).Error)
	asset := model.KKAIImageAsset{
		GenerationID: &generation.ID, OwnerUserID: 7, Scope: model.ImageAssetScopeUser,
		Kind: model.ImageAssetKindOutput, State: model.ImageAssetStateReady,
		ObjectKey: "private-key-must-not-leak", MIMEType: "image/png", SizeBytes: int64(len(payload)),
		Width: 1, Height: 1, SHA256: hex.EncodeToString(digest[:]), OriginalFilename: "../unsafe\r\nfile.png",
	}
	require.NoError(t, db.Create(&asset).Error)
	for _, test := range []struct {
		name   string
		userID int
		role   int
		data   []byte
		length int64
		err    error
		status int
		code   string
	}{
		{name: "original bytes", userID: 7, data: payload, length: int64(len(payload)), status: http.StatusOK},
		{name: "other user", userID: 8, data: payload, length: int64(len(payload)), status: http.StatusForbidden, code: "image_asset_access_denied"},
		{name: "admin cannot read other owner", userID: 8, role: common.RoleAdminUser, data: payload, length: int64(len(payload)), status: http.StatusForbidden, code: "image_asset_access_denied"},
		{name: "actual oversized despite forged length", userID: 7, data: append(append([]byte{}, payload...), make([]byte, 257)...), length: int64(len(payload)), status: http.StatusRequestEntityTooLarge, code: "image_asset_too_large"},
		{name: "short body never sends partial success", userID: 7, data: payload[:len(payload)/2], length: int64(len(payload)), status: http.StatusServiceUnavailable, code: "image_edit_source_unavailable"},
		{name: "storage errors redacted", userID: 7, err: errors.New("private-key-must-not-leak secret"), status: http.StatusServiceUnavailable, code: "image_edit_source_unavailable"},
		{name: "missing storage object", userID: 7, err: service.ErrVideoAssetObjectNotFound, status: http.StatusNotFound, code: "image_studio_resource_not_found"},
		{name: "storage timeout", userID: 7, err: fmt.Errorf("private-key-must-not-leak secret: %w", context.DeadlineExceeded), status: http.StatusGatewayTimeout, code: "image_edit_source_timeout"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Setenv("VIDEO_STUDIO_TEMP_DIR", tempDir)
			store := &imageOutputControllerStore{
				object: service.ImageAssetObject{Body: io.NopCloser(bytes.NewReader(test.data)), ContentType: "image/png", ContentLength: test.length},
				err:    test.err,
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			id := strconv.FormatInt(asset.ID, 10)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/image-studio/assets/"+id+"/edit-source", nil)
			c.Params = gin.Params{{Key: "id", Value: id}}
			c.Set("id", test.userID)
			c.Set("role", test.role)
			c.Set(imageStudioAssetStoreContextKey, store)
			GetImageStudioAssetEditSource(c)
			require.Equal(t, test.status, recorder.Code, recorder.Body.String())
			assert.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
			assert.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
			assert.Empty(t, recorder.Header().Get("Location"))
			assert.NotContains(t, recorder.Body.String(), asset.ObjectKey)
			entries, err := os.ReadDir(tempDir)
			require.NoError(t, err)
			assert.Empty(t, entries)
			if test.status != http.StatusOK {
				var response struct {
					Success bool   `json:"success"`
					Code    string `json:"code"`
				}
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
				assert.False(t, response.Success)
				assert.Equal(t, test.code, response.Code)
				return
			}
			assert.Equal(t, payload, recorder.Body.Bytes())
			assert.Equal(t, "image/png", recorder.Header().Get("Content-Type"))
			assert.Equal(t, strconv.Itoa(len(payload)), recorder.Header().Get("Content-Length"))
			disposition, parameters, err := mime.ParseMediaType(recorder.Header().Get("Content-Disposition"))
			require.NoError(t, err)
			assert.Equal(t, "inline", disposition)
			assert.Equal(t, "image-"+id+".png", parameters["filename"])
		})
	}
}
