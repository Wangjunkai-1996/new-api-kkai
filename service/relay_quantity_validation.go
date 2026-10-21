package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/tidwall/gjson"
)

// ValidateRelayOutboundQuantities checks the final JSON after channel overrides
// and passthrough conversion. It preserves the body, replay policy and closer.
func ValidateRelayOutboundQuantities(request *http.Request) error {
	if request == nil || request.Body == nil || request.Body == http.NoBody {
		return nil
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0]))
	jsonType := contentType == "application/json" || strings.HasSuffix(contentType, "+json")
	if contentType != "" && !jsonType {
		return nil
	}
	if encoding := strings.TrimSpace(request.Header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return fmt.Errorf("cannot verify encoded upstream JSON billing quantities")
	}
	reader := request.Body
	if request.GetBody != nil {
		var err error
		reader, err = request.GetBody()
		if err != nil {
			return err
		}
		defer reader.Close()
	}
	maxMB := constant.MaxRequestBodyMB
	if maxMB <= 0 {
		maxMB = 128 // Match common.GetRequestBody's shared BodyStorage limit.
	}
	limit := int64(maxMB) << 20
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if request.GetBody == nil {
		request.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(data), reader), reader}
	}
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("upstream JSON exceeds the shared request body limit")
	}
	if len(bytes.TrimSpace(data)) == 0 || !jsonType && !gjson.ValidBytes(data) {
		return nil
	}
	return ValidateRelayJSONQuantities(data)
}

// ValidateRelayJSONQuantities visits only protocol quantity fields and known
// request/config containers. Tool schemas, user content and extensions stay opaque.
func ValidateRelayJSONQuantities(data []byte) error {
	if !gjson.ValidBytes(data) {
		return fmt.Errorf("upstream billing quantities require valid JSON")
	}
	type object struct {
		value gjson.Result
		path  string
		kind  string
	}
	pending := []object{{value: gjson.ParseBytes(data), kind: "request"}}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if !current.value.IsObject() {
			continue
		}
		seen := make(map[string]bool)
		var invalid error
		current.value.ForEach(func(key, value gjson.Result) bool {
			name := key.String()
			path := name
			if current.path != "" {
				path = current.path + "." + name
			}
			limit, childKind := 0, ""
			if current.kind == "options" {
				if name == "num_predict" {
					limit = dto.MaxTokensLimit
				}
			} else {
				switch name {
				case "max_tokens", "max_completion_tokens", "max_output_tokens", "max_tokens_to_sample", "max_new_tokens", "maxTokens", "maxOutputTokens":
					limit = dto.MaxTokensLimit
				case "n", "candidateCount", "candidate_count":
					limit = dto.MaxImageN
				}
			}
			if current.kind == "request" {
				switch name {
				case "generationConfig", "generation_config", "parameters", "inferenceConfig":
					childKind = "config"
				case "options":
					childKind = "options"
				case "requests":
					childKind = "batch"
				}
			}
			if limit == 0 && childKind == "" {
				return true
			}
			if seen[name] {
				invalid = fmt.Errorf("duplicate upstream billing field %s", path)
				return false
			}
			seen[name] = true
			if limit > 0 && value.Type != gjson.Null {
				if !relayQuantityWithinLimit(value, limit) {
					invalid = fmt.Errorf("upstream %s must be an integer between 0 and %d", path, limit)
					return false
				}
			}
			if childKind == "batch" && value.IsArray() {
				value.ForEach(func(index, request gjson.Result) bool {
					pending = append(pending, object{request, path + "." + strconv.FormatInt(index.Int(), 10), "request"})
					return true
				})
			} else if childKind != "" && value.IsObject() {
				pending = append(pending, object{value, path, childKind})
			}
			return true
		})
		if invalid != nil {
			return invalid
		}
	}
	return nil
}

// Check the decimal spelling exactly: float parsing can round a fraction or a
// just-over-limit number to an allowed integer. No exponent-sized allocation.
func relayQuantityWithinLimit(value gjson.Result, limit int) bool {
	if value.Type != gjson.Number {
		return false
	}
	number := value.Raw
	negative := strings.HasPrefix(number, "-")
	number = strings.TrimPrefix(number, "-")
	exponentText := "0"
	if position := strings.IndexAny(number, "eE"); position >= 0 {
		exponentText = number[position+1:]
		number = number[:position]
	}
	fraction := 0
	if position := strings.IndexByte(number, '.'); position >= 0 {
		fraction = len(number) - position - 1
		number = number[:position] + number[position+1:]
	}
	number = strings.TrimLeft(number, "0")
	if number == "" {
		return true
	}
	if negative {
		return false
	}
	exponent, err := strconv.ParseInt(exponentText, 10, 64)
	if err != nil {
		return false
	}
	trimmed := strings.TrimRight(number, "0")
	// The only admissible exponents fit within the request's decimal digits;
	// checking before arithmetic also avoids an exponent subtraction overflow.
	if exponent < -int64(len(number)) || exponent > int64(fraction)+10 {
		return false
	}
	scale := exponent - int64(fraction) + int64(len(number)-len(trimmed))
	bound := strconv.Itoa(limit)
	if scale < 0 || int64(len(trimmed))+scale > int64(len(bound)) {
		return false
	}
	integer, err := strconv.Atoi(trimmed + strings.Repeat("0", int(scale)))
	return err == nil && integer <= limit
}
