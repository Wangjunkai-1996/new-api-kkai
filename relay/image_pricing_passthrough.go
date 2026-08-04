package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func buildPricedImagePassthroughBody(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	storage common.BodyStorage,
) (io.Reader, int64, io.Closer, error) {
	if c == nil || c.Request == nil || info == nil || info.ImagePricingSnapshot == nil || storage == nil {
		return nil, 0, nil, fmt.Errorf("invalid priced image passthrough request")
	}
	contentType := c.Request.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		body, err := storage.Bytes()
		if err != nil {
			return nil, 0, nil, err
		}
		var fields map[string]json.RawMessage
		if err := common.Unmarshal(body, &fields); err != nil {
			return nil, 0, nil, fmt.Errorf("decode priced image passthrough body: %w", err)
		}
		if fields == nil {
			return nil, 0, nil, fmt.Errorf("decode priced image passthrough body: JSON object is required")
		}
		fields["size"], err = common.Marshal(info.ImagePricingSnapshot.Size)
		if err != nil {
			return nil, 0, nil, err
		}
		fields["n"], err = common.Marshal(info.ImagePricingSnapshot.RequestedCount)
		if err != nil {
			return nil, 0, nil, err
		}
		body, err = common.Marshal(fields)
		if err != nil {
			return nil, 0, nil, err
		}
		if err := relaycommon.ValidateOutboundImagePricingJSON(info, body); err != nil {
			return nil, 0, nil, err
		}
		info.UpstreamIsStream = gjson.GetBytes(body, "stream").Bool()
		reader, sizeBytes, closer, err := relaycommon.NewOutboundJSONBody(body)
		return reader, sizeBytes, closer, err
	}
	if !strings.Contains(contentType, "multipart/form-data") || c.Request.MultipartForm == nil {
		return nil, 0, nil, fmt.Errorf("priced image passthrough requires JSON or parsed multipart data")
	}
	form := c.Request.MultipartForm
	values := url.Values(form.Value)
	count, err := strconv.Atoi(values.Get("n"))
	if err != nil {
		return nil, 0, nil, fmt.Errorf("invalid multipart image count: %w", err)
	}
	if err := relaycommon.ValidateOutboundImagePricingValues(info, values.Get("size"), count); err != nil {
		return nil, 0, nil, err
	}
	info.UpstreamIsStream, _ = strconv.ParseBool(values.Get("stream"))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fieldNames := make([]string, 0, len(form.Value))
	for name := range form.Value {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	for _, name := range fieldNames {
		for _, value := range form.Value[name] {
			if err := writer.WriteField(name, value); err != nil {
				return nil, 0, nil, fmt.Errorf("write multipart field %q: %w", name, err)
			}
		}
	}
	fileNames := make([]string, 0, len(form.File))
	for name := range form.File {
		fileNames = append(fileNames, name)
	}
	sort.Strings(fileNames)
	for _, name := range fileNames {
		for _, header := range form.File[name] {
			file, err := header.Open()
			if err != nil {
				return nil, 0, nil, fmt.Errorf("open multipart file %q: %w", name, err)
			}
			part, partErr := writer.CreatePart(header.Header)
			if partErr == nil {
				_, partErr = io.Copy(part, file)
			}
			closeErr := file.Close()
			if partErr != nil {
				return nil, 0, nil, fmt.Errorf("copy multipart file %q: %w", name, partErr)
			}
			if closeErr != nil {
				return nil, 0, nil, fmt.Errorf("close multipart file %q: %w", name, closeErr)
			}
		}
	}
	if err := writer.Close(); err != nil {
		return nil, 0, nil, fmt.Errorf("close multipart image body: %w", err)
	}
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return bytes.NewReader(body.Bytes()), int64(body.Len()), nil, nil
}
