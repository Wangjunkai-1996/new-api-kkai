package controller

import (
	"errors"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

var errImageRelayCaptureTooLarge = errors.New("image relay response exceeds capture limit")

type imageRelayCaptureWriter struct {
	gin.ResponseWriter
	header   http.Header
	file     *os.File
	path     string
	maxBytes int64
	status   int
	size     int
	err      error
	written  bool
}

func newImageRelayCaptureWriter(
	underlying gin.ResponseWriter,
	tempDir string,
	maxBytes int64,
) (*imageRelayCaptureWriter, error) {
	if underlying == nil || maxBytes <= 0 {
		return nil, errImageRelayCaptureTooLarge
	}
	file, err := os.CreateTemp(tempDir, "new-api-image-relay-response-*")
	if err != nil {
		return nil, err
	}
	return &imageRelayCaptureWriter{
		ResponseWriter: underlying, header: make(http.Header), file: file,
		path: file.Name(), maxBytes: maxBytes, status: http.StatusOK,
	}, nil
}

func (writer *imageRelayCaptureWriter) Header() http.Header {
	return writer.header
}

func (writer *imageRelayCaptureWriter) WriteHeader(status int) {
	if writer.written || status <= 0 {
		return
	}
	writer.status = status
	writer.written = true
}

func (writer *imageRelayCaptureWriter) WriteHeaderNow() {
	if !writer.written {
		writer.WriteHeader(writer.status)
	}
}

func (writer *imageRelayCaptureWriter) Write(body []byte) (int, error) {
	writer.WriteHeaderNow()
	if writer.err != nil {
		return 0, writer.err
	}
	if int64(writer.size)+int64(len(body)) > writer.maxBytes {
		writer.err = errImageRelayCaptureTooLarge
		return 0, writer.err
	}
	written, err := writer.file.Write(body)
	writer.size += written
	if err != nil {
		writer.err = err
	}
	return written, err
}

func (writer *imageRelayCaptureWriter) WriteString(body string) (int, error) {
	return writer.Write([]byte(body))
}

func (writer *imageRelayCaptureWriter) Status() int {
	return writer.status
}

func (writer *imageRelayCaptureWriter) Size() int {
	return writer.size
}

func (writer *imageRelayCaptureWriter) Written() bool {
	return writer.written
}

func (writer *imageRelayCaptureWriter) Flush() {
	writer.WriteHeaderNow()
}

func (writer *imageRelayCaptureWriter) Path() string {
	if writer == nil {
		return ""
	}
	return writer.path
}

func (writer *imageRelayCaptureWriter) Close() error {
	if writer == nil || writer.file == nil {
		return nil
	}
	err := writer.file.Close()
	writer.file = nil
	return err
}

func (writer *imageRelayCaptureWriter) Remove() {
	if writer == nil {
		return
	}
	_ = writer.Close()
	if writer.path != "" {
		_ = os.Remove(writer.path)
		writer.path = ""
	}
}
