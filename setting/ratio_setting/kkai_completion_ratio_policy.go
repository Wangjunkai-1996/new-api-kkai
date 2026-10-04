package ratio_setting

func resolveKKAICompletionRatio(name string) CompletionRatioInfo {
	name = FormatMatchingModelName(name)
	if ratio, ok := completionRatioMap.Get(name); ok {
		return CompletionRatioInfo{
			Ratio:  ratio,
			Locked: false,
		}
	}

	ratio, locked := getHardcodedCompletionModelRatio(name)
	return CompletionRatioInfo{
		Ratio:  ratio,
		Locked: locked,
	}
}

// ResolveCompletionRatio previews the same explicit-override-first policy used
// by settlement without changing the configured runtime map.
func ResolveCompletionRatio(name string, configured *float64) CompletionRatioInfo {
	if configured != nil {
		return CompletionRatioInfo{Ratio: *configured}
	}
	ratio, locked := getHardcodedCompletionModelRatio(FormatMatchingModelName(name))
	return CompletionRatioInfo{Ratio: ratio, Locked: locked}
}
