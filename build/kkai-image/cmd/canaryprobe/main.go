package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var canaryTokenPattern = regexp.MustCompile(`^sk-[A-Za-z0-9._-]{16,252}$`)

func main() {
	target := flag.String("url", "", "candidate URL")
	body := flag.String("body", "", "JSON request body")
	anthropicVersion := flag.String("anthropic-version", "", "optional Anthropic version header")
	flag.Parse()
	if err := run(os.Stdin, os.Stdout, *target, *body, *anthropicVersion); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "canary probe failed")
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer, target, body, anthropicVersion string) error {
	parsed, err := parseCandidateURL(target)
	if err != nil {
		return err
	}
	token, err := bufio.NewReader(io.LimitReader(input, 512)).ReadString('\n')
	token = strings.TrimSpace(token)
	if err != nil && !errors.Is(err, io.EOF) || !canaryTokenPattern.MatchString(token) {
		return errors.New("invalid canary token")
	}
	method := http.MethodGet
	var requestBody io.Reader
	if body != "" {
		method = http.MethodPost
		requestBody = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, parsed.String(), requestBody)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if anthropicVersion != "" {
		request.Header.Set("anthropic-version", anthropicVersion)
	}
	client := &http.Client{
		Timeout: 90 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("canary redirects are disabled")
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("candidate returned HTTP %d", response.StatusCode)
	}
	_, err = io.Copy(output, io.LimitReader(response.Body, 2*1024*1024))
	return err
}

func parseCandidateURL(target string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(target)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Port() != "3000" {
		return nil, errors.New("invalid canary target")
	}
	switch parsed.Hostname() {
	case "kkai-newapi-blue", "kkai-newapi-green":
		return parsed, nil
	default:
		return nil, errors.New("invalid canary target")
	}
}
