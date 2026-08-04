package service

import (
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/imagepricing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/image_pricing_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enableImageResolutionPricingForTest(t *testing.T) {
	t.Helper()
	original := image_pricing_setting.JSON()
	t.Cleanup(func() {
		require.NoError(t, image_pricing_setting.UpdateByJSONString(original))
	})

	config := image_pricing_setting.DefaultConfig()
	config.Enabled = true
	raw, err := common.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, image_pricing_setting.UpdateByJSONString(string(raw)))
}

func newImagePricingContext() *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	return context
}

func validTrustedImagePricingSnapshot() imagepricing.Snapshot {
	return imagepricing.Snapshot{
		PolicyVersion:     "quote-v1",
		PolicyHash:        "quote-hash",
		Model:             "gpt-image-2",
		Size:              "1024x1024",
		Tier:              "1k",
		UnitPrice:         0.67,
		QuotaPerUnit:      500000,
		GroupRatio:        1.2,
		GroupSpecialRatio: 0.8,
		HasSpecialRatio:   true,
		RequestedCount:    2,
	}
}

func TestPrepareImagePricingResolvesDefaultAndTierPrices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enableImageResolutionPricingForTest(t)

	tests := []struct {
		name      string
		size      string
		wantSize  string
		wantTier  string
		wantPrice float64
	}{
		{name: "missing size uses explicit 1k default", wantSize: "1024x1024", wantTier: "1k", wantPrice: 0.67},
		{name: "explicit 1k", size: "1024x1024", wantSize: "1024x1024", wantTier: "1k", wantPrice: 0.67},
		{name: "2k", size: "2048x2048", wantSize: "2048x2048", wantTier: "2k", wantPrice: 1},
		{name: "4k", size: "3840x2160", wantSize: "3840x2160", wantTier: "4k", wantPrice: 1.34},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := &dto.ImageRequest{Model: "gpt-image-2", Size: test.size}
			info := &relaycommon.RelayInfo{OriginModelName: "gpt-image-2", Request: request}

			err := PrepareImagePricing(newImagePricingContext(), info)
			require.NoError(t, err)
			require.NotNil(t, info.ImagePricingSnapshot)
			assert.Equal(t, test.wantSize, request.Size)
			assert.Equal(t, test.wantSize, info.ImagePricingSnapshot.Size)
			assert.Equal(t, test.wantTier, info.ImagePricingSnapshot.Tier)
			assert.Equal(t, test.wantPrice, info.ImagePricingSnapshot.UnitPrice)
			assert.Equal(t, 1, info.ImagePricingSnapshot.RequestedCount)
			assert.NotEmpty(t, info.ImagePricingSnapshot.PolicyVersion)
			assert.NotEmpty(t, info.ImagePricingSnapshot.PolicyHash)
		})
	}
}

func TestPrepareImagePricingRejectsUnpricedSizes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enableImageResolutionPricingForTest(t)

	for _, size := range []string{"auto", "1792x1024"} {
		t.Run(size, func(t *testing.T) {
			request := &dto.ImageRequest{Model: "gpt-image-2", Size: size}
			info := &relaycommon.RelayInfo{OriginModelName: "gpt-image-2", Request: request}

			err := PrepareImagePricing(newImagePricingContext(), info)
			assert.ErrorIs(t, err, imagepricing.ErrUnsupportedSize)
			assert.Nil(t, info.ImagePricingSnapshot)
		})
	}
}

func TestPrepareImagePricingUsesOnlyMatchingTrustedSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("matching snapshot is restored verbatim", func(t *testing.T) {
		context := newImagePricingContext()
		snapshot := validTrustedImagePricingSnapshot()
		require.NoError(t, SetTrustedImagePricingSnapshot(context, snapshot))
		count := uint(2)
		request := &dto.ImageRequest{Model: "gpt-image-2", Size: "1024x1024", N: &count}
		info := &relaycommon.RelayInfo{OriginModelName: "gpt-image-2", Request: request}

		require.NoError(t, PrepareImagePricing(context, info))
		require.NotNil(t, info.ImagePricingSnapshot)
		assert.Equal(t, snapshot, *info.ImagePricingSnapshot)
	})

	tests := []struct {
		name  string
		model string
		size  string
		count uint
	}{
		{name: "model mismatch", model: "other-model", size: "1024x1024", count: 2},
		{name: "size mismatch", model: "gpt-image-2", size: "2048x2048", count: 2},
		{name: "count mismatch", model: "gpt-image-2", size: "1024x1024", count: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context := newImagePricingContext()
			require.NoError(t, SetTrustedImagePricingSnapshot(context, validTrustedImagePricingSnapshot()))
			count := test.count
			request := &dto.ImageRequest{Model: test.model, Size: test.size, N: &count}
			info := &relaycommon.RelayInfo{OriginModelName: test.model, Request: request}

			err := PrepareImagePricing(context, info)
			assert.True(t, errors.Is(err, imagepricing.ErrOutboundMismatch), "unexpected error: %v", err)
			assert.Nil(t, info.ImagePricingSnapshot)
		})
	}
}

func TestPrepareImagePricingRejectsRepeatedMultipartPricingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enableImageResolutionPricingForTest(t)

	tests := []struct {
		name   string
		values map[string][]string
	}{
		{name: "repeated size", values: map[string][]string{"size": {"1024x1024", "3840x2160"}, "n": {"1"}}},
		{name: "repeated n", values: map[string][]string{"size": {"1024x1024"}, "n": {"1", "2"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context := newImagePricingContext()
			context.Request.MultipartForm = &multipart.Form{Value: test.values}
			request := &dto.ImageRequest{Model: "gpt-image-2", Size: "1024x1024"}
			info := &relaycommon.RelayInfo{OriginModelName: "gpt-image-2", Request: request}

			err := PrepareImagePricing(context, info)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "must not be repeated")
			assert.Nil(t, info.ImagePricingSnapshot)
		})
	}
}
