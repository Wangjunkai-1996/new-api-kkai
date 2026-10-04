package controller

import (
	"net/http"

	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
)

// RelayTaskPluginEndpoint dispatches a pinned protocol request to the host
// bridge and leaves unclaimed traffic on its existing relay handler.
func RelayTaskPluginEndpoint(c *gin.Context, fallback gin.HandlerFunc) {
	value, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint)
	if !exists {
		fallback(c)
		return
	}
	pinned, ok := value.(pluginruntime.PinnedEndpoint)
	if !ok || pinned.Plugin == nil || pinned.Generation == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "Task protocol request failed", "type": "new_api_error", "code": "task_protocol_error"}})
		return
	}
	switch pinned.Protocol {
	case "openai_responses":
		serveTaskPluginProtocol(c, pinned, defaultPluginProtocolBridgeDeps())
	case pluginruntime.ProtocolOpenAIImage:
		serveTaskPluginImageProtocol(c, pinned, defaultPluginProtocolBridgeDeps())
	default:
		fallback(c)
	}
}
