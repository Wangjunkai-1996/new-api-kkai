package relay

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func outboundImageCount(info *relaycommon.RelayInfo, body []byte, fallback int) (int, error) {
	if err := relaycommon.ValidateOutboundImagePricingJSON(info, body); err != nil {
		return 0, err
	}
	var outbound struct {
		N *uint `json:"n"`
	}
	if err := common.Unmarshal(body, &outbound); err != nil {
		return 0, fmt.Errorf("invalid image billing parameters: %w", err)
	}
	var path string
	switch info.GetChannelType() {
	case constant.ChannelTypeOpenAI:
		fallback = 1
	case constant.ChannelTypeSiliconFlow:
		path = "batch_size"
		fallback = 1
	case constant.ChannelTypeGemini, constant.ChannelTypeVertexAi:
		path = "parameters.sampleCount"
	case constant.ChannelTypeReplicate:
		path = "input.num_outputs"
	}
	if path != "" {
		value, err := imageQuantityField(body, path)
		if err != nil {
			return 0, err
		}
		if value.Exists() {
			count, err := strconv.ParseUint(value.Raw, 10, 32)
			if err != nil || count < 1 || count > dto.MaxImageN {
				return 0, fmt.Errorf("%s must be an integer between 1 and %d", path, dto.MaxImageN)
			}
			return int(count), nil
		}
		if path == "parameters.sampleCount" && ojson.GetBytesNoCopy(body, "instances").IsArray() {
			const imagenDefaultSampleCount = 4
			return imagenDefaultSampleCount, nil
		}
	}
	if outbound.N == nil {
		return fallback, nil
	}
	return (&dto.ImageRequest{N: outbound.N}).ImageCount(false)
}

func imageQuantityField(body []byte, path string) (ojson.Result, error) {
	object := ojson.GetBytesNoCopy(body, "@this")
	for key := range strings.SplitSeq(path, ".") {
		if !object.IsObject() {
			return ojson.Result{}, fmt.Errorf("invalid image billing field %s", path)
		}
		var field ojson.Result
		for name, value := range object.All() {
			if name.String() != key {
				continue
			}
			if field.Exists() {
				return ojson.Result{}, fmt.Errorf("ambiguous image billing field %s", path)
			}
			field = value
		}
		if !field.Exists() {
			return field, nil
		}
		object = field
	}
	return object, nil
}
