package riskguard

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type trackingBody struct {
	io.Reader
	closed bool
}

func (body *trackingBody) Close() error {
	body.closed = true
	return nil
}

func TestInspectRequestUsesOnlyLatestUserContentAndPreservesBody(t *testing.T) {
	body := `{"model":"gpt-test","messages":[{"role":"user","content":"old tcache __free_hook exploit"},{"role":"assistant","content":"tcache __free_hook exploit"},{"role":"user","content":"harmless current question"}]}`
	request, err := http.NewRequest(http.MethodPost, "http://guard/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	inspection, err := InspectRequest(request, 2*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Text != "harmless current question\n" || inspection.Model != "gpt-test" {
		t.Fatalf("unexpected inspection: %#v", inspection)
	}
	forwarded, err := io.ReadAll(request.Body)
	if err != nil || string(forwarded) != body {
		t.Fatalf("request body changed: %q, %v", forwarded, err)
	}
}

func TestInspectRequestSkipsOversizedAndMultipartBodies(t *testing.T) {
	oversized := `{"input":"` + strings.Repeat("x", 128) + `"}`
	request, _ := http.NewRequest(http.MethodPost, "http://guard/v1/responses", strings.NewReader(oversized))
	request.Header.Set("Content-Type", "application/json")
	inspection, err := InspectRequest(request, 32)
	if err != nil || inspection.Text != "" {
		t.Fatalf("oversized request was inspected: %#v, %v", inspection, err)
	}
	forwarded, _ := io.ReadAll(request.Body)
	if string(forwarded) != oversized {
		t.Fatal("oversized request body was not reconstructed")
	}

	multipart, _ := http.NewRequest(http.MethodPost, "http://guard/v1/files", strings.NewReader("tcache __free_hook exploit"))
	multipart.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	inspection, err = InspectRequest(multipart, 1024)
	if err != nil || inspection.Text != "" {
		t.Fatalf("multipart request was inspected: %#v, %v", inspection, err)
	}
}

func TestTokenFingerprintNeverReturnsRawCredential(t *testing.T) {
	token := "sk-sensitive-value-123456789"
	fingerprint := TokenFingerprint("Bearer " + token)
	if len(fingerprint) != 64 || strings.Contains(fingerprint, token) {
		t.Fatalf("unsafe fingerprint %q", fingerprint)
	}
}

func TestInspectRequestPreservesOriginalBodyCloser(t *testing.T) {
	original := &trackingBody{Reader: strings.NewReader(`{"input":"ok"}`)}
	request, _ := http.NewRequest(http.MethodPost, "http://guard/v1/responses", original)
	request.Header.Set("Content-Type", "application/json")
	if _, err := InspectRequest(request, 1024); err != nil {
		t.Fatal(err)
	}
	if err := request.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if !original.closed {
		t.Fatal("replayed request body did not close the original body")
	}
}
