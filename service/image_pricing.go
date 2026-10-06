package service

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/pkg/imagepricing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/image_pricing_setting"

	"github.com/gin-gonic/gin"
)

const trustedImagePricingSnapshotContextKey = "trusted_image_pricing_snapshot"

func PrepareImagePricing(c *gin.Context, info *relaycommon.RelayInfo) error {
	if info == nil {
		return nil
	}
	request, ok := info.Request.(*dto.ImageRequest)
	if !ok || request == nil {
		return nil
	}
	count := imageRequestCount(request)
	if count < 1 || count > dto.MaxImageN {
		return fmt.Errorf("invalid image count %d", count)
	}

	if trusted, ok := trustedImagePricingSnapshot(c); ok {
		if err := validateImagePricingMultipartDimensions(c); err != nil {
			return err
		}
		if trusted.Model != info.OriginModelName {
			return imagepricing.ErrOutboundMismatch
		}
		if err := imagepricing.ValidateOutbound(&trusted, request.Size, count); err != nil {
			return imagepricing.ErrOutboundMismatch
		}
		copy := trusted
		info.ImagePricingSnapshot = &copy
		syncImagePricingMultipartValues(c, request)
		return nil
	}

	match, configured, err := image_pricing_setting.Resolve(info.OriginModelName, request.Size)
	if err != nil {
		return err
	}
	if !configured {
		return nil
	}
	if err := validateImagePricingMultipartDimensions(c); err != nil {
		return err
	}
	request.Size = match.Resolution.Size
	info.ImagePricingSnapshot = &imagepricing.Snapshot{
		PolicyVersion:  match.PolicyVersion,
		PolicyHash:     match.PolicyHash,
		Model:          match.Resolution.Model,
		Size:           match.Resolution.Size,
		Tier:           match.Resolution.Tier,
		UnitPrice:      match.Resolution.UnitPrice,
		RequestedCount: count,
	}
	syncImagePricingMultipartValues(c, request)
	return nil
}

func validateImagePricingMultipartDimensions(c *gin.Context) error {
	if c == nil || c.Request == nil || c.Request.MultipartForm == nil {
		return nil
	}
	for _, key := range []string{"size", "n"} {
		if len(c.Request.MultipartForm.Value[key]) > 1 {
			return fmt.Errorf("image pricing field %q must not be repeated", key)
		}
	}
	return nil
}

func SetTrustedImagePricingSnapshot(c *gin.Context, snapshot imagepricing.Snapshot) error {
	if c == nil {
		return errors.New("image pricing context is required")
	}
	if err := imagepricing.ValidateSnapshot(&snapshot); err != nil {
		return err
	}
	c.Set(trustedImagePricingSnapshotContextKey, snapshot)
	return nil
}

func trustedImagePricingSnapshot(c *gin.Context) (imagepricing.Snapshot, bool) {
	if c == nil {
		return imagepricing.Snapshot{}, false
	}
	value, exists := c.Get(trustedImagePricingSnapshotContextKey)
	if !exists {
		return imagepricing.Snapshot{}, false
	}
	snapshot, ok := value.(imagepricing.Snapshot)
	return snapshot, ok
}

func imageRequestCount(request *dto.ImageRequest) int {
	if request == nil || request.N == nil || *request.N == 0 {
		return 1
	}
	return int(*request.N)
}

func syncImagePricingMultipartValues(c *gin.Context, request *dto.ImageRequest) {
	if c == nil || c.Request == nil || c.Request.MultipartForm == nil || request == nil {
		return
	}
	if c.Request.MultipartForm.Value == nil {
		c.Request.MultipartForm.Value = make(map[string][]string)
	}
	c.Request.MultipartForm.Value["size"] = []string{request.Size}
	c.Request.MultipartForm.Value["n"] = []string{strconv.Itoa(imageRequestCount(request))}
	c.Request.PostForm = c.Request.MultipartForm.Value
}
