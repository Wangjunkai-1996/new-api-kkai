package openai

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// Counts are local to one upstream attempt and committed only on success.
type responsesToolUsage struct {
	seen            map[string]bool
	webSearchCalls  int
	fileSearchCalls int
	webSearchTool   string
	images          []relaycommon.ResponsesImageGenerationCall
	imagesBillable  bool
}

func (u *responsesToolUsage) observeOutput(item *dto.ResponsesOutput, index *int) {
	if item == nil {
		return
	}
	switch item.Type {
	case dto.BuildInCallWebSearchCall, dto.BuildInCallFileSearchCall:
	case dto.ResponsesOutputTypeImageGenerationCall:
		if strings.TrimSpace(item.Result) == "" || (item.Status != "" && item.Status != "completed") {
			return
		}
	default:
		return
	}
	idKey := "id:" + item.ID
	indexKey := ""
	if index != nil && *index >= 0 {
		indexKey = "index:" + strconv.Itoa(*index)
	}
	if (item.ID != "" && u.seen[idKey]) || (indexKey != "" && u.seen[indexKey]) {
		return
	}
	if u.seen == nil {
		u.seen = make(map[string]bool)
	}
	if item.ID != "" {
		u.seen[idKey] = true
	}
	if indexKey != "" {
		u.seen[indexKey] = true
	}
	switch item.Type {
	case dto.BuildInCallWebSearchCall:
		u.webSearchCalls++
	case dto.BuildInCallFileSearchCall:
		u.fileSearchCalls++
	case dto.ResponsesOutputTypeImageGenerationCall:
		if len(u.images) < dto.MaxImageN {
			u.images = append(u.images, relaycommon.ResponsesImageGenerationCall{Quality: item.Quality, Size: item.Size})
		}
	}
}

func (u *responsesToolUsage) observeResponse(response *dto.OpenAIResponsesResponse) {
	if response == nil {
		return
	}
	// A populated final output is authoritative; otherwise retain item.done events.
	if len(response.Output) > 0 {
		*u = responsesToolUsage{}
		for i := range response.Output {
			u.observeOutput(&response.Output[i], &i)
		}
	}
	for _, tool := range response.Tools {
		switch common.Interface2String(tool["type"]) {
		case dto.BuildInToolWebSearchPreview, dto.BuildInToolWebSearch:
			u.webSearchTool = common.Interface2String(tool["type"])
		}
	}
	var status string
	_ = common.Unmarshal(response.Status, &status)
	u.imagesBillable = status == "" || status == "completed"
	if !u.imagesBillable {
		u.images = nil
	}
}

func (u *responsesToolUsage) observeEvent(event *dto.ResponsesStreamResponse) {
	switch event.Type {
	case dto.ResponsesOutputTypeItemDone:
		u.observeOutput(event.Item, event.OutputIndex)
	case "response.completed", "response.done", "response.incomplete":
		u.imagesBillable = event.Type != "response.incomplete"
		u.observeResponse(event.Response)
		if event.Type == "response.incomplete" {
			u.imagesBillable = false
			u.images = nil
		}
	}
}

func (u *responsesToolUsage) commit(info *relaycommon.RelayInfo, includeImages bool) {
	if info == nil {
		return
	}
	if info.ResponsesUsageInfo == nil {
		info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{}
	}
	if info.ResponsesUsageInfo.BuiltInTools == nil {
		info.ResponsesUsageInfo.BuiltInTools = make(map[string]*relaycommon.BuildInToolInfo)
	}
	tools := info.ResponsesUsageInfo.BuiltInTools
	webTool := u.webSearchTool
	if webTool == "" {
		if _, ok := tools[dto.BuildInToolWebSearchPreview]; ok {
			webTool = dto.BuildInToolWebSearchPreview
		} else if _, ok := tools[dto.BuildInToolWebSearch]; ok {
			webTool = dto.BuildInToolWebSearch
		}
	}
	if webTool == "" {
		webTool = dto.BuildInToolWebSearchPreview
	}
	for _, tool := range tools {
		if tool != nil {
			tool.CallCount = 0
		}
	}
	// Keep the existing settlement/log key, with the declared tool's price identity.
	if !u.imagesBillable || !includeImages {
		u.images = nil
	}
	tools[dto.BuildInToolWebSearchPreview] = &relaycommon.BuildInToolInfo{ToolName: webTool, CallCount: u.webSearchCalls}
	tools[dto.BuildInToolFileSearch] = &relaycommon.BuildInToolInfo{ToolName: dto.BuildInToolFileSearch, CallCount: u.fileSearchCalls}
	tools[dto.BuildInToolImageGeneration] = &relaycommon.BuildInToolInfo{ToolName: dto.BuildInToolImageGeneration, CallCount: len(u.images)}
	info.ResponsesUsageInfo.ImageGenerationCalls = append([]relaycommon.ResponsesImageGenerationCall{}, u.images...)
}
