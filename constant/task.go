package constant

type TaskPlatform string

const (
	TaskPlatformSuno       TaskPlatform = "suno"
	TaskPlatformMidjourney              = "mj"
)

const (
	SunoActionMusic  = "MUSIC"
	SunoActionLyrics = "LYRICS"

	TaskActionGenerate          = "generate"
	TaskActionTextGenerate      = "textGenerate"
	TaskActionFirstTailGenerate = "firstTailGenerate"
	TaskActionReferenceGenerate = "referenceGenerate"
	TaskActionRemix             = "remixGenerate"
	TaskActionImageToVideo      = "image_to_video"
	TaskActionTextToVideo       = "text_to_video"
	TaskActionFirstTailToVideo  = "first_tail_to_video"
	TaskActionReferenceToVideo  = "reference_to_video"
	TaskActionPluginRemix       = "remix"
)

var legacyTaskActionAliases = map[string]string{
	"generate":          TaskActionImageToVideo,
	"textGenerate":      TaskActionTextToVideo,
	"firstTailGenerate": TaskActionFirstTailToVideo,
	"referenceGenerate": TaskActionReferenceToVideo,
	"remixGenerate":     TaskActionPluginRemix,
}

// TaskPluginEnabled gates both built-in and user supplied task plugins.
var TaskPluginEnabled = true

func NormalizeTaskAction(action string) string {
	if canonical, ok := legacyTaskActionAliases[action]; ok {
		return canonical
	}
	return action
}

// VideoTaskActions includes persisted legacy names as well as canonical names.
func VideoTaskActions() []string {
	return []string{TaskActionImageToVideo, TaskActionTextToVideo, TaskActionFirstTailToVideo,
		TaskActionReferenceToVideo, TaskActionPluginRemix, TaskActionGenerate,
		TaskActionTextGenerate, TaskActionFirstTailGenerate, TaskActionReferenceGenerate, TaskActionRemix}
}

var SunoModel2Action = map[string]string{
	"suno_music":  SunoActionMusic,
	"suno_lyrics": SunoActionLyrics,
}
