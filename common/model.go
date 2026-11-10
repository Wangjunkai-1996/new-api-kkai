package common

import (
	"regexp"
	"strings"
)

var (
	// OpenAIResponseOnlyModels is a list of models that are only available for OpenAI responses.
	OpenAIResponseOnlyModels = []string{
		"o3-pro",
		"o3-deep-research",
		"o4-mini-deep-research",
	}
	ImageGenerationModels = []string{
		"dall-e-3",
		"dall-e-2",
		"prefix:imagen-",
		"flux-",
		"flux.1-",
	}
	gptImageModelPattern = regexp.MustCompile(`^gpt-image-[0-9]+(?:\.[0-9]+)*(?:-[a-z0-9]+(?:[._-][a-z0-9]+)*)?$`)
)

func IsOpenAIResponseOnlyModel(modelName string) bool {
	for _, m := range OpenAIResponseOnlyModels {
		if strings.Contains(modelName, m) {
			return true
		}
	}
	return false
}

func IsImageGenerationModel(modelName string) bool {
	modelName = strings.ToLower(modelName)
	if gptImageModelPattern.MatchString(modelName) {
		return true
	}
	for _, m := range ImageGenerationModels {
		if strings.Contains(modelName, m) {
			return true
		}
		if strings.HasPrefix(m, "prefix:") && strings.HasPrefix(modelName, strings.TrimPrefix(m, "prefix:")) {
			return true
		}
	}
	return false
}
