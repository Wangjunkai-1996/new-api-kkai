package common

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/pkg/imagepricing"

	"github.com/tidwall/gjson"
)

func ValidateOutboundImagePricingJSON(info *RelayInfo, body []byte) error {
	if info == nil || info.ImagePricingSnapshot == nil {
		return nil
	}
	sizeValue := gjson.GetBytes(body, "size")
	if !sizeValue.Exists() || sizeValue.Type != gjson.String {
		return fmt.Errorf("%w: outbound size is missing or invalid", imagepricing.ErrOutboundMismatch)
	}
	count := 1
	countValue := gjson.GetBytes(body, "n")
	if countValue.Exists() {
		parsed, err := strconv.ParseInt(countValue.Raw, 10, 32)
		if err != nil {
			return fmt.Errorf("%w: outbound n is invalid", imagepricing.ErrOutboundMismatch)
		}
		count = int(parsed)
	}
	return ValidateOutboundImagePricingValues(info, sizeValue.String(), count)
}

func ValidateOutboundImagePricingValues(info *RelayInfo, size string, count int) error {
	if info == nil || info.ImagePricingSnapshot == nil {
		return nil
	}
	if err := imagepricing.ValidateOutbound(info.ImagePricingSnapshot, size, count); err != nil {
		return err
	}
	info.ImagePricingOutboundValidated = true
	return nil
}
