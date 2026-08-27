package service

import (
	"errors"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

var ErrInvalidImageOutputCount = errors.New("invalid image output count")

// ApplyImageOutputBillingCount records the provider's usable output count for
// image adapters that do not return aggregate token usage.
func ApplyImageOutputBillingCount(info *relaycommon.RelayInfo, outputCount int) error {
	if info == nil || outputCount < 1 || outputCount > dto.MaxImageN {
		return ErrInvalidImageOutputCount
	}
	if request, ok := info.Request.(*dto.ImageRequest); ok {
		requestedCount := 1
		if request.N != nil {
			if *request.N < 1 || *request.N > uint(dto.MaxImageN) {
				return ErrInvalidImageOutputCount
			}
			requestedCount = int(*request.N)
		}
		if outputCount > requestedCount {
			return ErrInvalidImageOutputCount
		}
	}

	// AddOtherRatio replaces an existing value with the same key. Fixed-price
	// request counts are therefore corrected to the actual count, never stacked.
	info.PriceData.AddOtherRatio("n", float64(outputCount))
	return nil
}
