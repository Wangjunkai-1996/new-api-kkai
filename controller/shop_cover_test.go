package controller

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type shopCoverTestStore struct {
	objects map[string][]byte
	putErr  error
}

func (s *shopCoverTestStore) PresignDownload(context.Context, string, string, bool, time.Duration) (string, error) {
	return "", errors.New("covers must not issue storage URLs")
}
func (s *shopCoverTestStore) Delete(context.Context, []string) error { return nil }
func (s *shopCoverTestStore) Put(_ context.Context, key, mime string, reader io.Reader, size int64) error {
	if s.putErr != nil {
		return s.putErr
	}
	content, err := io.ReadAll(reader)
	if err == nil {
		s.objects[key] = content
	}
	return err
}
func (s *shopCoverTestStore) Get(_ context.Context, key string) (service.ImageAssetObject, error) {
	content, ok := s.objects[key]
	if !ok {
		return service.ImageAssetObject{}, service.ErrVideoAssetObjectNotFound
	}
	return service.ImageAssetObject{Body: io.NopCloser(bytes.NewReader(content)), ContentType: "image/jpeg", ContentLength: int64(len(content))}, nil
}

func TestShopCoverUploadAndPublicReadHTTPContract(t *testing.T) {
	store := &shopCoverTestStore{objects: map[string][]byte{}}
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set(imageStudioAssetStoreContextKey, store) })
	engine.POST("/api/admin/shop/covers", UploadShopCover)
	engine.GET("/api/shop/covers/:filename", GetShopCover)
	var source bytes.Buffer
	canvas := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	canvas.Set(0, 0, color.NRGBA{R: 255, A: 255})
	require.NoError(t, png.Encode(&source, canvas))
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "original.png")
	require.NoError(t, err)
	_, err = part.Write(source.Bytes())
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	upload := httptest.NewRequest(http.MethodPost, "/api/admin/shop/covers", &body)
	upload.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, upload)
	require.Equal(t, http.StatusCreated, recorder.Code, recorder.Body.String())
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	var result struct {
		Success bool              `json:"success"`
		Data    service.ShopCover `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
	assert.True(t, result.Success)
	assert.Equal(t, 4, result.Data.Width)
	assert.Equal(t, 3, result.Data.Height)
	assert.NotContains(t, recorder.Body.String(), "original.png")
	for _, conditional := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, result.Data.URL, nil)
		if conditional {
			req.Header.Set("If-None-Match", `"`+strings.TrimSuffix(strings.TrimPrefix(result.Data.URL, "/api/shop/covers/"), ".jpg")+`"`)
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		assert.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
		assert.Contains(t, response.Header().Get("Cache-Control"), "immutable")
		if conditional {
			assert.Equal(t, http.StatusNotModified, response.Code)
			assert.Empty(t, response.Body.Bytes())
		} else {
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, "image/jpeg", response.Header().Get("Content-Type"))
			_, _, err := image.Decode(bytes.NewReader(response.Body.Bytes()))
			assert.NoError(t, err)
		}
	}
	missing := httptest.NewRecorder()
	engine.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/shop/covers/"+strings.Repeat("a", 64)+".jpg", nil))
	assert.Equal(t, http.StatusNotFound, missing.Code)
	assert.Equal(t, "no-store", missing.Header().Get("Cache-Control"))
}

func TestShopCoverUploadRejectsAmbiguousFormsAndRedactsStorageFailure(t *testing.T) {
	var source bytes.Buffer
	require.NoError(t, png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	for _, tc := range []struct {
		name       string
		files      int
		extraValue bool
		putErr     error
		wantStatus int
		wantCode   string
	}{
		{"no-file", 0, false, nil, 400, "SHOP_COVER_INVALID"},
		{"two-files", 2, false, nil, 400, "SHOP_COVER_INVALID"},
		{"unexpected-fields", 1, true, nil, 400, "SHOP_COVER_INVALID"},
		{"storage-error", 1, false, errors.New("private-secret-in-storage-error"), 503, "SHOP_COVER_STORAGE_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			for i := 0; i < tc.files; i++ {
				part, err := writer.CreateFormFile("file", "cover.png")
				require.NoError(t, err)
				_, err = part.Write(source.Bytes())
				require.NoError(t, err)
			}
			if tc.extraValue {
				require.NoError(t, writer.WriteField("path", "private"))
			}
			require.NoError(t, writer.Close())
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Set(imageStudioAssetStoreContextKey, &shopCoverTestStore{objects: map[string][]byte{}, putErr: tc.putErr})
			ctx.Request = httptest.NewRequest(http.MethodPost, "/", &body)
			ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
			UploadShopCover(ctx)
			assert.Equal(t, tc.wantStatus, recorder.Code)
			assert.Contains(t, recorder.Body.String(), tc.wantCode)
			assert.NotContains(t, recorder.Body.String(), "private-secret")
		})
	}
}
