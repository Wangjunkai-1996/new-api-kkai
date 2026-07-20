package openai

import (
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func writeOpenAIImageJSONResponse(writer io.Writer, images [][]byte, responseMeta, usageRaw []byte, createdAt int64, responseFormat string) error {
	if createdAt <= 0 {
		createdAt = time.Now().Unix()
	}
	if _, err := io.WriteString(writer, `{"created":`+strconv.FormatInt(createdAt, 10)+`,"data":[`); err != nil {
		return err
	}
	for index, image := range images {
		if index > 0 {
			if _, err := io.WriteString(writer, ","); err != nil {
				return err
			}
		}
		if err := writeOpenAIImageJSONData(writer, image, responseFormat); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(writer, "]"); err != nil {
		return err
	}
	for _, field := range []string{"background", "output_format", "quality", "size", "model", "metadata"} {
		if err := writeOpenAIImageJSONRawField(writer, responseMeta, field); err != nil {
			return err
		}
	}
	if len(usageRaw) > 0 && gjson.ValidBytes(usageRaw) {
		if _, err := io.WriteString(writer, `,"usage":`+string(usageRaw)); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "}")
	return err
}

func writeOpenAIImageJSONData(writer io.Writer, image []byte, responseFormat string) error {
	if _, err := io.WriteString(writer, "{"); err != nil {
		return err
	}
	wroteField := false
	preferred := "b64_json"
	fallback := "url"
	if strings.EqualFold(strings.TrimSpace(responseFormat), "url") {
		preferred, fallback = fallback, preferred
	}
	if !openAIImageJSONStringExists(image, preferred) {
		preferred = fallback
	}
	for _, field := range []string{preferred, "revised_prompt"} {
		value := gjson.GetBytes(image, field)
		if value.Type != gjson.String {
			continue
		}
		if wroteField {
			if _, err := io.WriteString(writer, ","); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(writer, `"`+field+`":`+value.Raw); err != nil {
			return err
		}
		wroteField = true
	}
	_, err := io.WriteString(writer, "}")
	return err
}

func writeOpenAIImageJSONRawField(writer io.Writer, payload []byte, field string) error {
	value := gjson.GetBytes(payload, field)
	if !value.Exists() {
		return nil
	}
	_, err := io.WriteString(writer, `,"`+field+`":`+value.Raw)
	return err
}

func openAIImageJSONDataExists(payload []byte) bool {
	return openAIImageJSONStringExists(payload, "b64_json") || openAIImageJSONStringExists(payload, "url")
}

func openAIImageJSONStringExists(payload []byte, field string) bool {
	value := gjson.GetBytes(payload, field)
	return value.Type == gjson.String && value.String() != ""
}
