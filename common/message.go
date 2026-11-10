package common

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// Message is text shown in the web console. Key is the English source text and
// also its key in web/src/i18n/locales; Params fill its {{name}} placeholders.
// The web console translates Key with Params, so the backend only renders the
// English text, for API callers and log fallbacks. Params carry display-ready
// values: format quota with logger.FormatQuota before passing it.
type Message struct {
	Key    string         `json:"key"`
	Params map[string]any `json:"params,omitempty"`
}

func NewMessage(key string, params ...map[string]any) *Message {
	message := &Message{Key: key}
	if len(params) > 0 {
		message.Params = params[0]
	}
	return message
}

// Messages accepts legacy plain log notes as well as structured console notes.
// This keeps KKAI durable accounting callers on the same logging boundary.
func Messages(content any) []*Message {
	switch value := content.(type) {
	case nil:
		return nil
	case []*Message:
		return value
	case *Message:
		if value == nil {
			return nil
		}
		return []*Message{value}
	case []string:
		messages := make([]*Message, 0, len(value))
		for _, text := range value {
			if text != "" {
				messages = append(messages, NewMessage(text))
			}
		}
		return messages
	case string:
		if value == "" {
			return nil
		}
		return []*Message{NewMessage(value)}
	default:
		return []*Message{NewMessage(fmt.Sprint(value))}
	}
}

// Error renders the English text, so a Message can be returned as an error and
// reach the web console through ApiError.
func (m *Message) Error() string {
	if len(m.Params) == 0 {
		return m.Key
	}
	// One pass over the key, so a value that contains "{{name}}" stays as is.
	pairs := make([]string, 0, len(m.Params)*2)
	for name, value := range m.Params {
		pairs = append(pairs, "{{"+name+"}}", fmt.Sprint(value))
	}
	return strings.NewReplacer(pairs...).Replace(m.Key)
}

// Fields returns the response fields the web console translates. Callers put
// the English text in the field their response shape already uses.
func (m *Message) Fields() gin.H {
	fields := gin.H{"message_key": m.Key}
	if len(m.Params) > 0 {
		fields["message_params"] = m.Params
	}
	return fields
}
