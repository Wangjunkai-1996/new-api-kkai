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
