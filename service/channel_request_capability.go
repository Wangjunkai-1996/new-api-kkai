package service

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	imageStudioReferenceCountContextKey       = "image_studio_reference_count"
	imageStudioRequestedOutputCountContextKey = "image_studio_requested_output_count"
)

var (
	ErrNoChannelSupportsImageReferences  = errors.New("no available channel supports multiple image references")
	ErrNoChannelSupportsImageOutputCount = errors.New("no available channel supports requested image output count")

	imageStudioMultiReferenceChannelTypes = []int{
		constant.ChannelTypeOpenAI,
		constant.ChannelTypeAli,
	}
	imageStudioBatchOutputChannelTypes = []int{
		constant.ChannelTypeOpenAI,
		constant.ChannelTypeGemini,
		constant.ChannelTypeVertexAi,
		constant.ChannelTypeMiniMax,
		constant.ChannelTypeReplicate,
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

func SetImageStudioRequestedOutputCount(c *gin.Context, count int) {
	if c == nil {
		return
	}
	if count < 1 {
		count = 1
	}
	c.Set(imageStudioRequestedOutputCountContextKey, count)
}

func ImageStudioChannelMaxOutputs(channelType int) int {
	for _, batchChannelType := range imageStudioBatchOutputChannelTypes {
		if channelType == batchChannelType {
			return MaxImageStudioOutputs
		}
	}
	return 1
}

func ChannelMeetsRequestCapabilities(c *gin.Context, channel *model.Channel) bool {
	if channel == nil {
		return false
	}
	return ImageStudioRequestCapabilityError(c, channel) == nil
}

func ImageStudioRequestCapabilityError(c *gin.Context, channel *model.Channel) error {
	if channel == nil {
		return requestCapabilityError(c)
	}
	if imageStudioRequestedOutputCount(c) > ImageStudioChannelMaxOutputs(channel.Type) {
		return ErrNoChannelSupportsImageOutputCount
	}
	if imageStudioReferenceCount(c) > 1 && !imageStudioChannelSupportsMultipleReferences(channel.Type) {
		return ErrNoChannelSupportsImageReferences
	}
	return nil
}

func ValidateSelectedImageStudioChannel(c *gin.Context) error {
	if requestCapabilityError(c) == nil {
		return nil
	}
	return ImageStudioRequestCapabilityError(c, &model.Channel{
		Type: common.GetContextKeyInt(c, constant.ContextKeyChannelType),
	})
}

func channelTypesForRequest(c *gin.Context) []int {
	referenceCount := imageStudioReferenceCount(c)
	outputCount := imageStudioRequestedOutputCount(c)
	if referenceCount <= 1 && outputCount <= 1 {
		return nil
	}
	if outputCount <= 1 {
		return imageStudioMultiReferenceChannelTypes
	}
	if referenceCount <= 1 {
		return imageStudioBatchOutputChannelTypes
	}
	channelTypes := make([]int, 0, len(imageStudioMultiReferenceChannelTypes))
	for _, channelType := range imageStudioMultiReferenceChannelTypes {
		if ImageStudioChannelMaxOutputs(channelType) >= outputCount {
			channelTypes = append(channelTypes, channelType)
		}
	}
	return channelTypes
}

func requestCapabilityError(c *gin.Context) error {
	if imageStudioRequestedOutputCount(c) > 1 {
		return ErrNoChannelSupportsImageOutputCount
	}
	if imageStudioReferenceCount(c) > 1 {
		return ErrNoChannelSupportsImageReferences
	}
	return nil
}

func imageStudioRequestedOutputCount(c *gin.Context) int {
	if c == nil {
		return 1
	}
	count := c.GetInt(imageStudioRequestedOutputCountContextKey)
	if count < 1 {
		return 1
	}
	return count
}

func imageStudioReferenceCount(c *gin.Context) int {
	if c == nil {
		return 0
	}
	return c.GetInt(imageStudioReferenceCountContextKey)
}

func imageStudioChannelSupportsMultipleReferences(channelType int) bool {
	for _, supportedChannelType := range imageStudioMultiReferenceChannelTypes {
		if channelType == supportedChannelType {
			return true
		}
	}
	return false
}
