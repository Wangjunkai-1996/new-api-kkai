package billingexpr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSupportsMonotonicTokenEstimate(t *testing.T) {
	for _, test := range []struct {
		expression string
		supported  bool
	}{
		{`tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`, true},
		{`v1:tier("all", p + c + len + cr + cc + cc1h + img + img_cr + img_o + ai + ao)`, true},
		{`tier("free", 0)`, true},
		{`tier("polynomial", (p + 2) * c)`, true},
		{`tier("negative", p * -1)`, false},
		{`tier("subtract", p - c)`, false},
		{`tier("divide", p / (c + 1))`, false},
		{`tier("request", fixed(1)) * image_count`, false},
		{`tier("images", c * image_count)`, false},
		{`tier("request", p * float(param("n")))`, false},
		{`hour("UTC") < 12 ? tier("day", p) : tier("night", p * 2)`, false},
		{`true ? tier("ok", p) : tier("hidden", -c)`, false},
		{`tier(header("tier"), p)`, false},
		{`tier("unknown", unknown)`, false},
		{`tier("invalid", p`, false},
		{"", false},
	} {
		t.Run(test.expression, func(t *testing.T) {
			assert.Equal(t, test.supported, SupportsMonotonicTokenEstimate(test.expression))
		})
	}
}
