package openai

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// Counts are local to one upstream attempt and committed only on success.
type responsesToolUsage struct {
	seen            map[string]bool
	webSearchCalls  int
	fileSearchCalls int
	webSearchTool   string
	functionCalls   []string
	images          []relaycommon.ResponsesImageGenerationCall
	imagesBillable  bool
}

func (u *responsesToolUsage) observeOutput(item *dto.ResponsesOutput, index *int) {
	if item == nil {
		return
	}
	switch item.Type {
	case dto.BuildInCallWebSearchCall, dto.BuildInCallFileSearchCall, dto.BuildInCallFunctionCall:
	case dto.ResponsesOutputTypeImageGenerationCall:
		if strings.TrimSpace(item.Result) == "" || (item.Status != "" && item.Status != "completed") {
			return
		}
	default:
		return
	}
	aliases := make([]string, 0, 4)
	if item.ID != "" {
		aliases = append(aliases, "id:"+item.ID)
	}
	if item.CallId != "" {
		aliases = append(aliases, "call:"+item.CallId)
	}
	if index != nil && *index >= 0 {
		aliases = append(aliases, "index:"+strconv.Itoa(*index))
	}
	if item.Type == dto.ResponsesOutputTypeImageGenerationCall {
		digest := sha256.Sum256([]byte(item.Result))
		aliases = append(aliases, "result:"+hex.EncodeToString(digest[:]))
	}
	for _, alias := range aliases {
		if u.seen[alias] {
			return
		}
	}
	if u.seen == nil {
		u.seen = make(map[string]bool)
	}
	for _, alias := range aliases {
		u.seen[alias] = true
	}
	switch item.Type {
	case dto.BuildInCallWebSearchCall:
		u.webSearchCalls++
	case dto.BuildInCallFileSearchCall:
		u.fileSearchCalls++
	case dto.BuildInCallFunctionCall:
		u.functionCalls = append(u.functionCalls, item.Name)
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
	for _, name := range u.functionCalls {
		info.CountBillableToolCall(dto.BuildInCallFunctionCall, name)
	}
	info.ResponsesUsageInfo.ImageGenerationCalls = append([]relaycommon.ResponsesImageGenerationCall{}, u.images...)
}
