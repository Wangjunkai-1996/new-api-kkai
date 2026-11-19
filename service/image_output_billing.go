package service

import (
	"errors"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

var ErrInvalidImageOutputCount = errors.New("invalid image output count")

// RecordImageOutputCount validates and records the provider's usable output
// count without changing token-based billing ratios.
func RecordImageOutputCount(info *relaycommon.RelayInfo, outputCount int) error {
	if info == nil || outputCount < 1 || outputCount > dto.MaxImageN {
		return ErrInvalidImageOutputCount
	}
	if info.ImageRequestCount != 0 {
		if outputCount > info.ImageRequestCount || info.ImageRequestCount > dto.MaxImageN {
			return ErrInvalidImageOutputCount
		}
	} else if request, ok := info.Request.(*dto.ImageRequest); ok {
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
	info.ImageOutputCount = outputCount
	info.UpdateImageCount(int64(outputCount))
	return nil
}

// ApplyImageOutputBillingCount records the provider's usable output count for
// image adapters that do not return aggregate token usage.
func ApplyImageOutputBillingCount(info *relaycommon.RelayInfo, outputCount int) error {
	if err := RecordImageOutputCount(info, outputCount); err != nil {
		return err
	}

	if !info.PriceData.UsePrice && info.TieredBillingSnapshot == nil {
		info.PriceData.AddOtherRatio("n", float64(outputCount))
	}
	return nil
}
