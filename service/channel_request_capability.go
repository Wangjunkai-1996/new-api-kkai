package service

import (
	"errors"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const imageStudioReferenceCountContextKey = "image_studio_reference_count"

var (
	ErrNoChannelSupportsImageReferences   = errors.New("no available channel supports multiple image references")
	imageStudioMultiReferenceChannelTypes = []int{
		constant.ChannelTypeOpenAI,
		constant.ChannelTypeAli,
	}
)

func SetImageStudioReferenceCount(c *gin.Context, count int) {
	if c == nil {
		return
	}
	if count < 0 {
		count = 0
	}
	c.Set(imageStudioReferenceCountContextKey, count)
}

func ChannelMeetsRequestCapabilities(c *gin.Context, channel *model.Channel) bool {
	if channel == nil {
		return false
	}
	allowedTypes := channelTypesForRequest(c)
	if len(allowedTypes) == 0 {
		return true
	}
	for _, channelType := range allowedTypes {
		if channel.Type == channelType {
			return true
		}
	}
	return false
}

func channelTypesForRequest(c *gin.Context) []int {
	if c == nil || c.GetInt(imageStudioReferenceCountContextKey) <= 1 {
		return nil
	}
	return imageStudioMultiReferenceChannelTypes
}
