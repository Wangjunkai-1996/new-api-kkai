package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestRunKeepsTokenOutOfURLAndBody(t *testing.T) {
	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "http://kkai-newapi-blue:3000/v1/responses" {
			t.Fatalf("unexpected URL %s", request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer sk-test-canary-token" {
			t.Fatal("authorization header is missing")
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"input":"ok"}` || strings.Contains(string(body), "sk-test") {
			t.Fatalf("unsafe body %q", body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Header:     make(http.Header),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	var output bytes.Buffer
	err := run(strings.NewReader("sk-test-canary-token\n"), &output,
		"http://kkai-newapi-blue:3000/v1/responses", `{"input":"ok"}`, "")
	if err != nil || output.String() != `{"ok":true}` {
		t.Fatalf("unexpected probe result %q, %v", output.String(), err)
	}
}

func TestRunRejectsTargetsOutsideManagedCandidateSlots(t *testing.T) {
	originalTransport := http.DefaultTransport
	requestCount := 0
	http.DefaultTransport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		requestCount++
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	tests := []string{
		"https://kkai-newapi-blue:3000/v1/responses",
		"http://kkai-newapi-blue.example:3000/v1/responses",
		"http://user@kkai-newapi-blue:3000/v1/responses",
		"http://kkai-newapi-blue:8080/v1/responses",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			err := run(strings.NewReader("sk-test-canary-token\n"), io.Discard, target, "", "")
			if err == nil {
				t.Fatalf("accepted untrusted target %q", target)
			}
		})
	}
	if requestCount != 0 {
		t.Fatalf("sent credentials to %d untrusted targets", requestCount)
	}
}

func TestRunRejectsRedirectsBeforeCredentialForwarding(t *testing.T) {
	originalTransport := http.DefaultTransport
	requestCount := 0
	http.DefaultTransport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestCount++
		return &http.Response{
			StatusCode: http.StatusTemporaryRedirect,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     http.Header{"Location": []string{"http://example.com/capture"}},
			Request:    request,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	err := run(strings.NewReader("sk-test-canary-token\n"), io.Discard,
		"http://kkai-newapi-green:3000/api/usage/token/", "", "")
	if err == nil {
		t.Fatal("canary probe followed an HTTP redirect")
	}
	if requestCount != 1 {
		t.Fatalf("canary probe issued %d requests after a redirect", requestCount)
	}
}
