package service

import "github.com/QuantumNous/new-api/tokenkit"

// InitTokenEncoders retains the service startup hook while TokenKit owns all
// encoders and counting. Its codecs are also safe to initialize lazily.
func InitTokenEncoders() {
	tokenkit.Count("gpt-4", "warmup")
	tokenkit.Count("gpt-4o", "warmup")
}

// CountTextToken preserves the service API used by KKAI billing callers.
func CountTextToken(text, model string) int {
	return tokenkit.Count(model, text)
}

func EstimateTokenByModel(model, text string) int {
	return tokenkit.Estimate(model, text)
}
