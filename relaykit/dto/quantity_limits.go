package dto

import "math"

// MaxTokensLimit bounds billing quantities before they can overflow quota math.
const MaxTokensLimit = math.MaxInt32 / 2
