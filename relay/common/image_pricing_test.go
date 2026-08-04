package common

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/imagepricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func outboundImagePricingSnapshot() *imagepricing.Snapshot {
	return &imagepricing.Snapshot{
		PolicyVersion:  "policy-v1",
		PolicyHash:     "policy-hash",
		Model:          "gpt-image-2",
		Size:           "1024x1024",
		Tier:           "1k",
		UnitPrice:      0.67,
		QuotaPerUnit:   500000,
		GroupRatio:     1,
		RequestedCount: 2,
	}
}

func TestValidateOutboundImagePricingJSONLocksSizeAndCount(t *testing.T) {
	info := &RelayInfo{ImagePricingSnapshot: outboundImagePricingSnapshot()}

	require.NoError(t, ValidateOutboundImagePricingJSON(info, []byte(`{"size":"1024x1024","n":2}`)))
	assert.True(t, info.ImagePricingOutboundValidated)

	tests := []struct {
		name string
		body string
	}{
		{name: "missing size", body: `{"n":2}`},
		{name: "non string size", body: `{"size":1024,"n":2}`},
		{name: "changed size", body: `{"size":"3840x2160","n":2}`},
		{name: "missing count defaults to one", body: `{"size":"1024x1024"}`},
		{name: "changed count", body: `{"size":"1024x1024","n":3}`},
		{name: "non numeric count", body: `{"size":"1024x1024","n":"2"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := &RelayInfo{ImagePricingSnapshot: outboundImagePricingSnapshot()}

			err := ValidateOutboundImagePricingJSON(info, []byte(test.body))
			assert.ErrorIs(t, err, imagepricing.ErrOutboundMismatch)
			assert.False(t, info.ImagePricingOutboundValidated)
		})
	}
}
