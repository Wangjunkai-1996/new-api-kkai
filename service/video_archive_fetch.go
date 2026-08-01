package service

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

var (
	ErrVideoArchiveSourceRejected       = errors.New("video archive source is not allowed")
	ErrVideoArchiveResponseRejected     = errors.New("video archive source returned an invalid response")
	ErrVideoArchiveMIMERejected         = errors.New("video archive source returned an unsupported media type")
	ErrVideoArchiveTooLarge             = errors.New("video archive source exceeds the configured size limit")
	ErrVideoTemporaryStorageUnavailable = errors.New("video temporary storage is unavailable")
)

type videoArchiveHTTPStatusError struct {
	statusCode int
}

func (err *videoArchiveHTTPStatusError) Error() string {
	return fmt.Sprintf("video archive source returned HTTP status %d", err.statusCode)
}

func (err *videoArchiveHTTPStatusError) Unwrap() error {
	return ErrVideoArchiveResponseRejected
}

func (err *videoArchiveHTTPStatusError) retryable() bool {
	if err == nil {
		return false
	}
	switch err.statusCode {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	default:
		return err.statusCode >= http.StatusInternalServerError && err.statusCode <= 599
	}
}

const videoTemporaryStorageReserveBytes uint64 = 64 << 20

type VideoArchiveSourceFetcher interface {
	Fetch(context.Context, string, int64) (*FetchedVideoArchive, error)
}

type videoArchiveTaskSource struct {
	Source                 string
	Headers                map[string]string
	ProxyURL               string
	ProviderContentBaseURL string
}

type videoArchiveTaskSourceFetcher interface {
	FetchTaskSource(context.Context, videoArchiveTaskSource, int64) (*FetchedVideoArchive, error)
}

type FetchedVideoArchive struct {
	Path      string
	MIMEType  string
	SizeBytes int64
	SHA256    string
}

func (archive *FetchedVideoArchive) Remove() {
	if archive == nil || archive.Path == "" {
		return
	}
	_ = os.Remove(archive.Path)
	archive.Path = ""
}

type HTTPVideoArchiveFetcher struct {
	client           *http.Client
	providerClient   func(string) (*http.Client, error)
	tempDir          string
	availableBytes   func(string) (uint64, error)
	validateURL      func(context.Context, *url.URL) error
	proxyResolver    ssrfResolver
	proxyDialContext func(context.Context, string, string) (net.Conn, error)
	tlsConfig        *tls.Config
}

func NewHTTPVideoArchiveFetcher(tempDir string) *HTTPVideoArchiveFetcher {
	protection := strictVideoArchiveProtection()
	netDialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	client := newProtectedFetchHTTPClientWithProxy(
		nil,
		nil,
		func() (*common.SSRFProtection, bool, error) { return protection, true, nil },
		func(*http.Request) (*url.URL, error) { return nil, nil },
	)
	fetcher := &HTTPVideoArchiveFetcher{
		client:           client,
		providerClient:   configuredProviderContentHTTPClient,
		tempDir:          strings.TrimSpace(tempDir),
		availableBytes:   videoTemporaryAvailableBytes,
		validateURL:      validateStrictVideoArchiveURL,
		proxyResolver:    net.DefaultResolver,
		proxyDialContext: netDialer.DialContext,
	}
	if common.TLSInsecureSkipVerify {
		fetcher.tlsConfig = common.InsecureTLSConfig.Clone()
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return ErrVideoArchiveSourceRejected
		}
		return fetcher.validateURL(request.Context(), request.URL)
	}
	return fetcher
}

func (fetcher *HTTPVideoArchiveFetcher) Fetch(ctx context.Context, source string, maxBytes int64) (*FetchedVideoArchive, error) {
	return fetcher.fetchURL(ctx, videoArchiveTaskSource{Source: source}, maxBytes)
}

func (fetcher *HTTPVideoArchiveFetcher) FetchTaskSource(ctx context.Context, source videoArchiveTaskSource, maxBytes int64) (*FetchedVideoArchive, error) {
	if strings.HasPrefix(strings.TrimSpace(source.Source), "data:") {
		return fetcher.fetchDataURL(source.Source, maxBytes)
	}
	return fetcher.fetchURL(ctx, source, maxBytes)
}

func (fetcher *HTTPVideoArchiveFetcher) fetchURL(ctx context.Context, source videoArchiveTaskSource, maxBytes int64) (*FetchedVideoArchive, error) {
	if fetcher == nil || fetcher.client == nil || fetcher.availableBytes == nil || fetcher.validateURL == nil || maxBytes <= 0 {
		return nil, ErrVideoArchiveSourceRejected
	}
	rawSource := strings.TrimSpace(source.Source)
	for name, value := range source.Headers {
		if strings.EqualFold(name, "x-goog-api-key") && strings.TrimSpace(value) != "" {
			rawSource = VideoSourceWithoutProviderCredentialQuery(rawSource)
			break
		}
	}
	parsed, err := url.ParseRequestURI(rawSource)
	if err != nil || parsed == nil || !parsed.IsAbs() {
		return nil, ErrVideoArchiveSourceRejected
	}
	configuredProviderContent := false
	if err := fetcher.validateURL(ctx, parsed); err != nil {
		configuredProviderContent = matchesConfiguredProviderContentURL(parsed, source.ProviderContentBaseURL)
		if !configuredProviderContent || len(source.Headers) == 0 {
			if errors.Is(err, ErrVideoArchiveSourceRejected) {
				return nil, ErrVideoArchiveSourceRejected
			}
			return nil, fmt.Errorf("validate video archive source: %w", err)
		}
	}
	if configuredProviderContent && fetcher.providerClient == nil {
		return nil, ErrVideoArchiveSourceRejected
	}
	available, err := fetcher.availableBytes(fetcher.tempDir)
	if err != nil || available < uint64(maxBytes)+videoTemporaryStorageReserveBytes {
		return nil, ErrVideoTemporaryStorageUnavailable
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, ErrVideoArchiveSourceRejected
	}
	request.Header.Set("Accept", "video/mp4,video/webm,video/quicktime")
	for name, value := range source.Headers {
		request.Header.Set(name, value)
	}
	client, err := fetcher.clientForTaskSource(source.ProxyURL, len(source.Headers) > 0, configuredProviderContent)
	if err != nil {
		return nil, ErrVideoArchiveSourceRejected
	}
	if len(source.Headers) > 0 {
		baseClient := client
		clientCopy := *baseClient
		baseRedirect := baseClient.CheckRedirect
		clientCopy.CheckRedirect = func(redirect *http.Request, via []*http.Request) error {
			if len(via) > 0 {
				return ErrVideoArchiveSourceRejected
			}
			if baseRedirect != nil {
				return baseRedirect(redirect, via)
			}
			return nil
		}
		client = &clientCopy
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch video archive source: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &videoArchiveHTTPStatusError{statusCode: response.StatusCode}
	}
	if response.ContentLength > maxBytes {
		return nil, ErrVideoArchiveTooLarge
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !isSupportedVideoMIME(strings.ToLower(mediaType)) {
		return nil, ErrVideoArchiveMIMERejected
	}
	return fetcher.writeArchiveFile(response.Body, strings.ToLower(mediaType), maxBytes)
}

func (fetcher *HTTPVideoArchiveFetcher) clientForTaskSource(proxyURL string, pinDirectTarget bool, configuredProviderContent bool) (*http.Client, error) {
	if configuredProviderContent {
		client, err := fetcher.providerClient(strings.TrimSpace(proxyURL))
		if err != nil || client == nil {
			return nil, ErrVideoArchiveSourceRejected
		}
		return client, nil
	}
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" && !pinDirectTarget {
		return fetcher.client, nil
	}
	var parsedProxy *url.URL
	if proxyURL != "" {
		var err error
		parsedProxy, err = url.Parse(proxyURL)
		if err != nil || parsedProxy == nil || parsedProxy.Hostname() == "" {
			return nil, ErrVideoArchiveSourceRejected
		}
		switch parsedProxy.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return nil, ErrVideoArchiveSourceRejected
		}
	}
	resolver := fetcher.proxyResolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialContext := fetcher.proxyDialContext
	if dialContext == nil {
		netDialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		dialContext = netDialer.DialContext
	}
	client := &http.Client{
		Transport: &videoArchivePinnedRoundTripper{
			proxyURL:      parsedProxy,
			resolver:      resolver,
			dialContext:   dialContext,
			baseTLSConfig: fetcher.tlsConfig,
		},
	}
	if fetcher.client != nil {
		client.Timeout = fetcher.client.Timeout
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return ErrVideoArchiveSourceRejected
		}
		return fetcher.validateURL(request.Context(), request.URL)
	}
	return client, nil
}

func configuredProviderContentHTTPClient(proxyURL string) (*http.Client, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		return GetHttpClientWithProxy(proxyURL)
	}
	baseClient := GetHttpClient()
	if baseClient == nil {
		return nil, ErrVideoArchiveSourceRejected
	}
	baseTransport := baseClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	httpTransport, ok := baseTransport.(*http.Transport)
	if !ok || httpTransport == nil {
		return nil, ErrVideoArchiveSourceRejected
	}
	transportCopy := httpTransport.Clone()
	transportCopy.Proxy = nil
	clientCopy := *baseClient
	clientCopy.Transport = transportCopy
	return &clientCopy, nil
}

func matchesConfiguredProviderContentURL(source *url.URL, rawProviderBase string) bool {
	providerBase, err := url.ParseRequestURI(strings.TrimSpace(rawProviderBase))
	if err != nil || source == nil || providerBase == nil || !source.IsAbs() || !providerBase.IsAbs() ||
		source.Hostname() == "" || providerBase.Hostname() == "" || source.User != nil || providerBase.User != nil ||
		source.Fragment != "" || providerBase.Fragment != "" || source.RawQuery != "" || providerBase.RawQuery != "" {
		return false
	}
	sourceScheme := strings.ToLower(source.Scheme)
	providerScheme := strings.ToLower(providerBase.Scheme)
	if (sourceScheme != "http" && sourceScheme != "https") || sourceScheme != providerScheme ||
		!strings.EqualFold(source.Hostname(), providerBase.Hostname()) {
		return false
	}
	sourcePort := source.Port()
	if sourcePort == "" {
		if sourceScheme == "https" {
			sourcePort = "443"
		} else {
			sourcePort = "80"
		}
	}
	providerPort := providerBase.Port()
	if providerPort == "" {
		if providerScheme == "https" {
			providerPort = "443"
		} else {
			providerPort = "80"
		}
	}
	if sourcePort != providerPort {
		return false
	}

	basePath := strings.TrimSuffix(providerBase.EscapedPath(), "/")
	prefix := basePath + "/v1/videos/"
	sourcePath := source.EscapedPath()
	if !strings.HasPrefix(sourcePath, prefix) || !strings.HasSuffix(sourcePath, "/content") {
		return false
	}
	escapedTaskID := strings.TrimSuffix(strings.TrimPrefix(sourcePath, prefix), "/content")
	decodedTaskID, err := url.PathUnescape(escapedTaskID)
	if err != nil || decodedTaskID == "" || len(decodedTaskID) > 256 || decodedTaskID == "." || decodedTaskID == ".." {
		return false
	}
	for _, character := range decodedTaskID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' || character == '-' || character == '.' || character == '~' {
			continue
		}
		return false
	}
	if url.PathEscape(decodedTaskID) != escapedTaskID {
		return false
	}
	return sourcePath == prefix+url.PathEscape(decodedTaskID)+"/content"
}

type videoArchivePinnedRoundTripper struct {
	proxyURL      *url.URL
	resolver      ssrfResolver
	dialContext   func(context.Context, string, string) (net.Conn, error)
	baseTLSConfig *tls.Config
}

func (transport *videoArchivePinnedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport == nil || transport.resolver == nil || transport.dialContext == nil || request == nil || request.URL == nil {
		return nil, ErrVideoArchiveSourceRejected
	}
	addresses, err := resolveStrictVideoArchiveTarget(request.Context(), request.URL, transport.resolver)
	if err != nil {
		return nil, err
	}

	originHost := request.URL.Host
	serverName := request.URL.Hostname()
	// RoundTrip may send credentials before reporting an error. Task-level retry
	// must perform the next resolution instead of replaying against another IP.
	pinnedRequest := request.Clone(request.Context())
	pinnedURL := *request.URL
	pinnedURL.Host = net.JoinHostPort(addresses[0].String(), "443")
	pinnedRequest.URL = &pinnedURL
	pinnedRequest.Host = originHost

	proxyTransport := transport.newTransport(serverName)
	response, roundTripErr := proxyTransport.RoundTrip(pinnedRequest)
	if roundTripErr != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		proxyTransport.CloseIdleConnections()
		if request.Context().Err() != nil {
			return nil, request.Context().Err()
		}
		return nil, fmt.Errorf("connect to validated video archive target: %w", roundTripErr)
	}
	if response == nil {
		return nil, ErrVideoArchiveResponseRejected
	}
	response.Request = request
	return response, nil
}

func (transport *videoArchivePinnedRoundTripper) newTransport(targetServerName string) *http.Transport {
	targetTLSConfig := transport.cloneTLSConfig()
	targetTLSConfig.ServerName = targetServerName
	proxyTransport := &http.Transport{
		MaxIdleConns:        common.RelayMaxIdleConns,
		MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
		IdleConnTimeout:     time.Duration(common.RelayIdleConnTimeout) * time.Second,
		DisableKeepAlives:   true,
		ForceAttemptHTTP2:   true,
		DialContext:         transport.dialContext,
		TLSClientConfig:     targetTLSConfig,
	}
	if transport.proxyURL != nil {
		proxyTransport.Proxy = http.ProxyURL(transport.proxyURL)
	}
	if transport.proxyURL != nil && transport.proxyURL.Scheme == "https" {
		proxyTLSConfig := transport.cloneTLSConfig()
		proxyTLSConfig.ServerName = transport.proxyURL.Hostname()
		proxyTLSConfig.NextProtos = []string{"http/1.1"}
		proxyTransport.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			connection, err := transport.dialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			tlsConnection := tls.Client(connection, proxyTLSConfig)
			if err := tlsConnection.HandshakeContext(ctx); err != nil {
				_ = connection.Close()
				return nil, err
			}
			return tlsConnection, nil
		}
	}
	return proxyTransport
}

func (transport *videoArchivePinnedRoundTripper) cloneTLSConfig() *tls.Config {
	if transport.baseTLSConfig == nil {
		return &tls.Config{}
	}
	return transport.baseTLSConfig.Clone()
}

func (fetcher *HTTPVideoArchiveFetcher) fetchDataURL(source string, maxBytes int64) (*FetchedVideoArchive, error) {
	if fetcher == nil || fetcher.availableBytes == nil || maxBytes <= 0 {
		return nil, ErrVideoArchiveSourceRejected
	}
	header, payload, found := strings.Cut(strings.TrimSpace(source), ",")
	if !found || !strings.HasPrefix(header, "data:") || !strings.HasSuffix(strings.ToLower(header), ";base64") {
		return nil, ErrVideoArchiveSourceRejected
	}
	mediaType := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	mediaType, _, err := mime.ParseMediaType(mediaType)
	if err != nil || !isSupportedVideoMIME(strings.ToLower(mediaType)) {
		return nil, ErrVideoArchiveMIMERejected
	}
	if int64(base64.StdEncoding.DecodedLen(len(payload))) > maxBytes {
		return nil, ErrVideoArchiveTooLarge
	}
	available, err := fetcher.availableBytes(fetcher.tempDir)
	if err != nil || available < uint64(maxBytes)+videoTemporaryStorageReserveBytes {
		return nil, ErrVideoTemporaryStorageUnavailable
	}
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload))
	return fetcher.writeArchiveFile(decoder, strings.ToLower(mediaType), maxBytes)
}

func (fetcher *HTTPVideoArchiveFetcher) writeArchiveFile(reader io.Reader, mediaType string, maxBytes int64) (*FetchedVideoArchive, error) {

	file, err := os.CreateTemp(fetcher.tempDir, "new-api-video-archive-*")
	if err != nil {
		return nil, ErrVideoTemporaryStorageUnavailable
	}
	path := file.Name()
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()

	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read video archive source: %w", err)
	}
	if written <= 0 {
		return nil, ErrVideoArchiveResponseRejected
	}
	if written > maxBytes {
		return nil, ErrVideoArchiveTooLarge
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close video archive temporary file: %w", err)
	}
	remove = false
	return &FetchedVideoArchive{
		Path: path, MIMEType: strings.ToLower(mediaType), SizeBytes: written,
		SHA256: hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func validateStrictVideoArchiveURL(ctx context.Context, parsed *url.URL) error {
	_, err := resolveStrictVideoArchiveTarget(ctx, parsed, net.DefaultResolver)
	return err
}

func resolveStrictVideoArchiveTarget(ctx context.Context, parsed *url.URL, resolver ssrfResolver) ([]net.IP, error) {
	if parsed == nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, ErrVideoArchiveSourceRejected
	}
	port := 443
	if parsed.Port() != "" && parsed.Port() != "443" {
		return nil, ErrVideoArchiveSourceRejected
	}
	protection := strictVideoArchiveProtection()
	if err := protection.ValidateNetworkTarget(parsed.Hostname(), port); err != nil {
		return nil, ErrVideoArchiveSourceRejected
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil {
		return []net.IP{append(net.IP(nil), ip...)}, nil
	}
	if resolver == nil {
		return nil, ErrVideoArchiveSourceRejected
	}
	addresses, err := resolver.LookupIPAddr(ctx, parsed.Hostname())
	if err != nil {
		return nil, fmt.Errorf("resolve video archive source: %w", err)
	}
	if len(addresses) == 0 {
		return nil, errors.New("video archive source DNS resolution returned no addresses")
	}
	validated := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		ip := address.IP.To4()
		if ip == nil {
			ip = address.IP.To16()
		}
		if ip == nil {
			return nil, ErrVideoArchiveSourceRejected
		}
		if err := protection.ValidateResolvedIP(parsed.Hostname(), ip); err != nil {
			return nil, ErrVideoArchiveSourceRejected
		}
		validated = append(validated, append(net.IP(nil), ip...))
	}
	return validated, nil
}

// VideoSourceCanUseProviderCredentials restricts provider secrets to the
// configured HTTPS origin. Cross-origin media URLs are fetched without secrets.
func VideoSourceCanUseProviderCredentials(source string, providerBase string) bool {
	sourceOrigin, sourceOK := canonicalHTTPSOrigin(source)
	providerOrigin, providerOK := canonicalHTTPSOrigin(providerBase)
	return sourceOK && providerOK && sourceOrigin == providerOrigin
}

// VideoSourceWithoutProviderCredentialQuery removes API-key query parameters
// that must be carried in provider request headers instead.
func VideoSourceWithoutProviderCredentialQuery(source string) string {
	parsed, err := url.Parse(strings.TrimSpace(source))
	if err != nil || parsed == nil {
		return source
	}
	query := parsed.Query()
	for name := range query {
		switch strings.ToLower(name) {
		case "key", "api_key", "api-key", "apikey", "x-goog-api-key":
			query.Del(name)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func canonicalHTTPSOrigin(rawURL string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || !parsed.IsAbs() || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return "", false
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	return "https://" + net.JoinHostPort(strings.ToLower(parsed.Hostname()), port), true
}

func strictVideoArchiveProtection() *common.SSRFProtection {
	return &common.SSRFProtection{
		AllowPrivateIp: false, DomainFilterMode: false, IpFilterMode: false,
		AllowedPorts: []int{443}, ApplyIPFilterForDomain: true,
	}
}

func videoTemporaryAvailableBytes(path string) (uint64, error) {
	return videoAvailableBytesForPath(path)
}
