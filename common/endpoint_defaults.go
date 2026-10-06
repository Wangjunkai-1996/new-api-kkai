package common

import (
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
)

// EndpointInfo 描述单个端点的默认请求信息
// path: 上游路径
// method: HTTP 请求方式，例如 POST/GET
// 目前均为 POST，后续可扩展
//
// json 标签用于直接序列化到 API 输出
// 例如：{"path":"/v1/chat/completions","method":"POST"}

type EndpointInfo struct {
	Path   string `json:"path"`
	Method string `json:"method"`
}

// defaultEndpointInfoMap 保存内置端点的默认 Path 与 Method
var defaultEndpointInfoMap = map[relaytypes.EndpointType]EndpointInfo{
	relaytypes.EndpointTypeOpenAI:                {Path: "/v1/chat/completions", Method: "POST"},
	relaytypes.EndpointTypeOpenAIResponse:        {Path: "/v1/responses", Method: "POST"},
	relaytypes.EndpointTypeOpenAIResponseCompact: {Path: "/v1/responses/compact", Method: "POST"},
	relaytypes.EndpointTypeOpenAIAlphaSearch:     {Path: "/v1/alpha/search", Method: "POST"},
	relaytypes.EndpointTypeAnthropic:             {Path: "/v1/messages", Method: "POST"},
	relaytypes.EndpointTypeGemini:                {Path: "/v1beta/models/{model}:generateContent", Method: "POST"},
	relaytypes.EndpointTypeJinaRerank:            {Path: "/v1/rerank", Method: "POST"},
	relaytypes.EndpointTypeImageGeneration:       {Path: "/v1/images/generations", Method: "POST"},
	relaytypes.EndpointTypeEmbeddings:            {Path: "/v1/embeddings", Method: "POST"},
}

// GetDefaultEndpointInfo 返回指定端点类型的默认信息以及是否存在
func GetDefaultEndpointInfo(et relaytypes.EndpointType) (EndpointInfo, bool) {
	info, ok := defaultEndpointInfoMap[et]
	return info, ok
}
