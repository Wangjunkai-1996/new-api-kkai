package service

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type videoArchiveRoundTripper func(*http.Request) (*http.Response, error)

func (transport videoArchiveRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type contextBlockingVideoArchiveBody struct {
	ctx context.Context
}

func (body contextBlockingVideoArchiveBody) Read([]byte) (int, error) {
	<-body.ctx.Done()
	return 0, body.ctx.Err()
}

func (contextBlockingVideoArchiveBody) Close() error {
	return nil
}

func TestHTTPVideoArchiveFetcherRejectsUnsafeSourcesBeforeRequest(t *testing.T) {
	requests := 0
	fetcher := &HTTPVideoArchiveFetcher{
		client: &http.Client{Transport: videoArchiveRoundTripper(func(*http.Request) (*http.Response, error) {
			requests++
			return nil, nil
		})},
		tempDir:        t.TempDir(),
		availableBytes: func(string) (uint64, error) { return 1 << 30, nil },
		validateURL:    validateStrictVideoArchiveURL,
	}

	_, err := fetcher.Fetch(context.Background(), "http://example.com/video.mp4", 1024)
	require.ErrorIs(t, err, ErrVideoArchiveSourceRejected)
	_, err = fetcher.Fetch(context.Background(), "https://127.0.0.1/video.mp4", 1024)
	require.ErrorIs(t, err, ErrVideoArchiveSourceRejected)
	require.Zero(t, requests)
}

func TestHTTPVideoArchiveFetcherEnforcesMIMEAndStreamLimit(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantErr     error
	}{
		{name: "unsupported MIME", contentType: "text/html", body: "not video", wantErr: ErrVideoArchiveMIMERejected},
		{name: "stream exceeds limit", contentType: "video/mp4", body: strings.Repeat("x", 33), wantErr: ErrVideoArchiveTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fetcher := newVideoArchiveTestFetcher(t, test.contentType, test.body)
			_, err := fetcher.Fetch(context.Background(), "https://media.example/video.mp4", 32)
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestHTTPVideoArchiveFetcherWritesBoundedTemporaryFile(t *testing.T) {
	fetcher := newVideoArchiveTestFetcher(t, "video/mp4", "video-bytes")
	fetched, err := fetcher.Fetch(context.Background(), "https://media.example/video.mp4", 1024)
	require.NoError(t, err)
	t.Cleanup(func() { fetched.Remove() })

	require.Equal(t, "video/mp4", fetched.MIMEType)
	require.EqualValues(t, len("video-bytes"), fetched.SizeBytes)
	require.Len(t, fetched.SHA256, 64)
	content, err := os.ReadFile(fetched.Path)
	require.NoError(t, err)
	require.Equal(t, "video-bytes", string(content))
}

func TestHTTPVideoArchiveFetcherFailsClosedWhenTemporaryDiskIsLow(t *testing.T) {
	fetcher := newVideoArchiveTestFetcher(t, "video/mp4", "video-bytes")
	fetcher.availableBytes = func(string) (uint64, error) { return 1, nil }

	_, err := fetcher.Fetch(context.Background(), "https://media.example/video.mp4", 1024)
	require.ErrorIs(t, err, ErrVideoTemporaryStorageUnavailable)
}

func TestHTTPVideoArchiveFetcherHonorsDeadlineWhileWaitingForHeaders(t *testing.T) {
	fetcher := newVideoArchiveTestFetcher(t, "video/mp4", "unused")
	fetcher.client.Transport = videoArchiveRoundTripper(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	_, err := fetcher.Fetch(ctx, "https://media.example/video.mp4", 1024)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(startedAt), time.Second)
}

func TestHTTPVideoArchiveFetcherHonorsDeadlineWhileReadingBody(t *testing.T) {
	tempDir := t.TempDir()
	fetcher := newVideoArchiveTestFetcher(t, "video/mp4", "unused")
	fetcher.tempDir = tempDir
	fetcher.client.Transport = videoArchiveRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"video/mp4"}},
			Body:       contextBlockingVideoArchiveBody{ctx: request.Context()},
			Request:    request,
		}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	_, err := fetcher.Fetch(ctx, "https://media.example/video.mp4", 1024)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	entries, readErr := os.ReadDir(tempDir)
	require.NoError(t, readErr)
	require.Empty(t, entries)
}

func TestHTTPVideoArchiveFetcherRejectsMixedProxyTargetResolutionBeforeDial(t *testing.T) {
	tests := []struct {
		name      string
		addresses []net.IPAddr
	}{
		{
			name: "mixed public and private results",
			addresses: []net.IPAddr{
				{IP: net.ParseIP("8.8.8.8")},
				{IP: net.ParseIP("127.0.0.1")},
			},
		},
		{name: "malformed IP result", addresses: []net.IPAddr{{IP: net.IP{1, 2, 3}}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fetcher := newVideoArchiveTestFetcher(t, "video/mp4", "unused")
			fetcher.proxyResolver = staticSSRFResolver{"media.example": test.addresses}
			var dialCount atomic.Int32
			fetcher.proxyDialContext = func(context.Context, string, string) (net.Conn, error) {
				dialCount.Add(1)
				return nil, fmt.Errorf("unexpected proxy dial")
			}

			_, err := fetcher.FetchTaskSource(context.Background(), videoArchiveTaskSource{
				Source:   "https://media.example/video.mp4",
				ProxyURL: "http://proxy.example:8080",
			}, 1024)

			require.ErrorIs(t, err, ErrVideoArchiveSourceRejected)
			require.Zero(t, dialCount.Load())
		})
	}
}

func TestHTTPVideoArchiveFetcherProxyPinsValidatedIPAndPreservesOrigin(t *testing.T) {
	tests := []struct {
		name     string
		newProxy func(*testing.T, string) videoArchiveProxyFixture
	}{
		{name: "http", newProxy: newVideoArchiveHTTPConnectProxy},
		{name: "https", newProxy: newVideoArchiveHTTPSConnectProxy},
		{name: "socks5", newProxy: func(t *testing.T, originAddress string) videoArchiveProxyFixture {
			return newVideoArchiveSOCKS5Proxy(t, originAddress, "socks5")
		}},
		{name: "socks5h", newProxy: func(t *testing.T, originAddress string) videoArchiveProxyFixture {
			return newVideoArchiveSOCKS5Proxy(t, originAddress, "socks5h")
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			origin := newVideoArchiveObservedTLSServer(t)
			proxy := test.newProxy(t, origin.server.Listener.Addr().String())
			fetcher := newVideoArchiveTestFetcher(t, "video/mp4", "unused")
			fetcher.proxyResolver = staticSSRFResolver{
				"media.example": {{IP: net.ParseIP("8.8.8.8")}},
			}
			fetcher.proxyDialContext = proxy.dialContext
			fetcher.tlsConfig = &tls.Config{InsecureSkipVerify: true} // test certificate

			fetched, err := fetcher.FetchTaskSource(context.Background(), videoArchiveTaskSource{
				Source:   "https://media.example/video.mp4",
				ProxyURL: proxy.url,
			}, 1024)
			require.NoError(t, err)
			t.Cleanup(fetched.Remove)

			require.Equal(t, "8.8.8.8:443", <-proxy.target)
			require.Equal(t, "media.example", <-origin.sni)
			require.Equal(t, "media.example", <-origin.host)
			if proxy.sni != nil {
				require.Equal(t, "proxy.example", <-proxy.sni)
			}
		})
	}
}

func TestHTTPVideoArchiveFetcherProxyRejectsNonStandardTargetPortBeforeDial(t *testing.T) {
	fetcher := newVideoArchiveTestFetcher(t, "video/mp4", "unused")
	fetcher.proxyResolver = staticSSRFResolver{
		"media.example": {{IP: net.ParseIP("8.8.8.8")}},
	}
	var dialCount atomic.Int32
	fetcher.proxyDialContext = func(context.Context, string, string) (net.Conn, error) {
		dialCount.Add(1)
		return nil, fmt.Errorf("unexpected proxy dial")
	}

	_, err := fetcher.FetchTaskSource(context.Background(), videoArchiveTaskSource{
		Source:   "https://media.example:8443/video.mp4",
		ProxyURL: "socks5://proxy.example:1080",
	}, 1024)

	require.ErrorIs(t, err, ErrVideoArchiveSourceRejected)
	require.Zero(t, dialCount.Load())
}

func TestHTTPVideoArchiveFetcherProxyPinsEachRedirectTarget(t *testing.T) {
	sni := make(chan string, 2)
	host := make(chan string, 2)
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		host <- request.Host
		if request.Host == "first.example" {
			http.Redirect(writer, request, "https://second.example/video.mp4", http.StatusFound)
			return
		}
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = io.WriteString(writer, "video-bytes")
	}))
	origin.TLS = &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sni <- hello.ServerName
			return nil, nil
		},
	}
	origin.StartTLS()
	t.Cleanup(origin.Close)

	target := make(chan string, 2)
	proxy := httptest.NewServer(videoArchiveConnectProxyHandler(origin.Listener.Addr().String(), target))
	t.Cleanup(proxy.Close)
	fetcher := newVideoArchiveTestFetcher(t, "video/mp4", "unused")
	fetcher.proxyResolver = staticSSRFResolver{
		"first.example":  {{IP: net.ParseIP("8.8.8.8")}},
		"second.example": {{IP: net.ParseIP("1.1.1.1")}},
	}
	fetcher.tlsConfig = &tls.Config{InsecureSkipVerify: true} // test certificate

	fetched, err := fetcher.FetchTaskSource(context.Background(), videoArchiveTaskSource{
		Source:   "https://first.example/video.mp4",
		ProxyURL: proxy.URL,
	}, 1024)
	require.NoError(t, err)
	t.Cleanup(fetched.Remove)

	require.Equal(t, "8.8.8.8:443", <-target)
	require.Equal(t, "1.1.1.1:443", <-target)
	require.Equal(t, "first.example", <-sni)
	require.Equal(t, "second.example", <-sni)
	require.Equal(t, "first.example", <-host)
	require.Equal(t, "second.example", <-host)
}

func TestHTTPVideoArchiveFetcherProxyDoesNotReplayRequestAcrossValidatedIPs(t *testing.T) {
	var requestCount atomic.Int32
	authorization := make(chan string, 2)
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount.Add(1)
		authorization <- request.Header.Get("Authorization") + ":" + request.Header.Get("x-goog-api-key")
		connection, _, err := writer.(http.Hijacker).Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}))
	origin.StartTLS()
	t.Cleanup(origin.Close)

	var connectCount atomic.Int32
	target := make(chan string, 2)
	proxyHandler := videoArchiveConnectProxyHandler(origin.Listener.Addr().String(), target)
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connectCount.Add(1)
		proxyHandler.ServeHTTP(writer, request)
	}))
	t.Cleanup(proxy.Close)

	fetcher := newVideoArchiveTestFetcher(t, "video/mp4", "unused")
	fetcher.proxyResolver = staticSSRFResolver{
		"media.example": {
			{IP: net.ParseIP("8.8.8.8")},
			{IP: net.ParseIP("1.1.1.1")},
		},
	}
	fetcher.tlsConfig = &tls.Config{InsecureSkipVerify: true} // test certificate

	_, err := fetcher.FetchTaskSource(context.Background(), videoArchiveTaskSource{
		Source: "https://media.example/video.mp4",
		Headers: map[string]string{
			"Authorization":  "Bearer secret",
			"x-goog-api-key": "api-secret",
		},
		ProxyURL: proxy.URL,
	}, 1024)

	require.Error(t, err)
	require.EqualValues(t, 1, connectCount.Load())
	require.EqualValues(t, 1, requestCount.Load())
	require.Equal(t, "8.8.8.8:443", <-target)
	require.Equal(t, "Bearer secret:api-secret", <-authorization)
}

func TestHTTPVideoArchiveFetcherDirectCredentialsPinTargetAndPreserveOrigin(t *testing.T) {
	origin := newVideoArchiveObservedTLSServer(t)
	fetcher := newVideoArchiveTestFetcher(t, "video/mp4", "unused")
	fetcher.client = &http.Client{Transport: &http.Transport{}}
	fetcher.proxyResolver = staticSSRFResolver{
		"media.example": {{IP: net.ParseIP("8.8.8.8")}},
	}
	dialed := make(chan string, 1)
	dialer := &net.Dialer{}
	fetcher.proxyDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed <- address
		return dialer.DialContext(ctx, network, origin.server.Listener.Addr().String())
	}
	fetcher.tlsConfig = &tls.Config{InsecureSkipVerify: true} // test certificate

	fetched, err := fetcher.FetchTaskSource(context.Background(), videoArchiveTaskSource{
		Source: "https://media.example/video.mp4?key=query-secret&alt=media",
		Headers: map[string]string{
			"Authorization":  "Bearer secret",
			"x-goog-api-key": "api-secret",
		},
	}, 1024)
	require.NoError(t, err)
	t.Cleanup(fetched.Remove)

	require.Equal(t, "8.8.8.8:443", <-dialed)
	require.Equal(t, "media.example", <-origin.sni)
	require.Equal(t, "media.example", <-origin.host)
	require.Equal(t, "/video.mp4?alt=media", <-origin.uri)
}

func TestHTTPVideoArchiveFetcherDirectCredentialsDoNotReplayAfterReusedConnectionFailure(t *testing.T) {
	credentialHeaders := make(chan string, 2)
	credentialHost := make(chan string, 2)
	firstRequestCount := atomic.Int32{}
	firstOrigin := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if firstRequestCount.Add(1) == 1 {
			writer.Header().Set("Content-Type", "video/mp4")
			_, _ = io.WriteString(writer, "warm-connection")
			return
		}
		credentialHeaders <- request.Header.Get("Authorization") + ":" + request.Header.Get("x-goog-api-key")
		credentialHost <- request.Host
		connection, _, err := writer.(http.Hijacker).Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}))
	firstOrigin.StartTLS()
	t.Cleanup(firstOrigin.Close)

	secondRequestCount := atomic.Int32{}
	secondOrigin := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		secondRequestCount.Add(1)
		credentialHeaders <- request.Header.Get("Authorization") + ":" + request.Header.Get("x-goog-api-key")
		credentialHost <- request.Host
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = io.WriteString(writer, "replayed-request")
	}))
	t.Cleanup(secondOrigin.Close)

	sharedDialCount := atomic.Int32{}
	netDialer := &net.Dialer{}
	sharedTransport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // test certificates
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			if sharedDialCount.Add(1) == 1 {
				return netDialer.DialContext(ctx, network, firstOrigin.Listener.Addr().String())
			}
			return netDialer.DialContext(ctx, network, secondOrigin.Listener.Addr().String())
		},
	}
	t.Cleanup(sharedTransport.CloseIdleConnections)

	fetcher := newVideoArchiveTestFetcher(t, "video/mp4", "unused")
	fetcher.client = &http.Client{Transport: sharedTransport}
	fetcher.proxyResolver = staticSSRFResolver{
		"media.example": {
			{IP: net.ParseIP("8.8.8.8")},
			{IP: net.ParseIP("1.1.1.1")},
		},
	}
	pinnedDials := make(chan string, 2)
	fetcher.proxyDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		pinnedDials <- address
		switch address {
		case "8.8.8.8:443":
			return netDialer.DialContext(ctx, network, firstOrigin.Listener.Addr().String())
		case "1.1.1.1:443":
			return netDialer.DialContext(ctx, network, secondOrigin.Listener.Addr().String())
		default:
			return nil, fmt.Errorf("unexpected pinned dial %s", address)
		}
	}
	fetcher.tlsConfig = &tls.Config{InsecureSkipVerify: true} // test certificates

	warm, err := fetcher.Fetch(context.Background(), "https://media.example/video.mp4", 1024)
	require.NoError(t, err)
	warm.Remove()

	_, err = fetcher.FetchTaskSource(context.Background(), videoArchiveTaskSource{
		Source: "https://media.example/video.mp4",
		Headers: map[string]string{
			"Authorization":  "Bearer secret",
			"x-goog-api-key": "api-secret",
		},
	}, 1024)

	require.Error(t, err)
	require.EqualValues(t, 2, firstRequestCount.Load())
	require.Zero(t, secondRequestCount.Load())
	require.Equal(t, "Bearer secret:api-secret", <-credentialHeaders)
	require.Equal(t, "media.example", <-credentialHost)
	require.Equal(t, "8.8.8.8:443", <-pinnedDials)
	require.EqualValues(t, 1, sharedDialCount.Load())
}

type videoArchiveObservedTLSServer struct {
	server *httptest.Server
	sni    chan string
	host   chan string
	uri    chan string
}

func newVideoArchiveObservedTLSServer(t *testing.T) videoArchiveObservedTLSServer {
	t.Helper()
	sni := make(chan string, 1)
	host := make(chan string, 1)
	uri := make(chan string, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		host <- request.Host
		uri <- request.URL.String()
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = io.WriteString(writer, "video-bytes")
	}))
	server.TLS = &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sni <- hello.ServerName
			return nil, nil
		},
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	return videoArchiveObservedTLSServer{server: server, sni: sni, host: host, uri: uri}
}

type videoArchiveProxyFixture struct {
	url         string
	dialContext func(context.Context, string, string) (net.Conn, error)
	target      chan string
	sni         chan string
}

func newVideoArchiveHTTPConnectProxy(t *testing.T, originAddress string) videoArchiveProxyFixture {
	t.Helper()
	target := make(chan string, 1)
	server := httptest.NewServer(videoArchiveConnectProxyHandler(originAddress, target))
	t.Cleanup(server.Close)
	return videoArchiveProxyFixture{url: server.URL, target: target}
}

func newVideoArchiveHTTPSConnectProxy(t *testing.T, originAddress string) videoArchiveProxyFixture {
	t.Helper()
	target := make(chan string, 1)
	sni := make(chan string, 1)
	server := httptest.NewUnstartedServer(videoArchiveConnectProxyHandler(originAddress, target))
	server.TLS = &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sni <- hello.ServerName
			return nil, nil
		},
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	proxyAddress := net.JoinHostPort("proxy.example", parsed.Port())
	dialer := &net.Dialer{}
	return videoArchiveProxyFixture{
		url:    "https://" + proxyAddress,
		target: target,
		sni:    sni,
		dialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != proxyAddress {
				return nil, fmt.Errorf("unexpected dial address %s", address)
			}
			return dialer.DialContext(ctx, network, server.Listener.Addr().String())
		},
	}
}

func videoArchiveConnectProxyHandler(originAddress string, target chan<- string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodConnect {
			http.Error(writer, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		target <- request.Host
		clientConnection, buffered, err := writer.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		originConnection, err := net.Dial("tcp", originAddress)
		if err != nil {
			_ = clientConnection.Close()
			return
		}
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffered.Flush()
		go func() {
			_, _ = io.Copy(originConnection, clientConnection)
			_ = originConnection.Close()
		}()
		go func() {
			_, _ = io.Copy(clientConnection, originConnection)
			_ = clientConnection.Close()
		}()
	})
}

func newVideoArchiveSOCKS5Proxy(t *testing.T, originAddress string, scheme string) videoArchiveProxyFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	target := make(chan string, 1)

	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		greeting := make([]byte, 2)
		if _, err := io.ReadFull(connection, greeting); err != nil || greeting[0] != 5 {
			return
		}
		methods := make([]byte, int(greeting[1]))
		if _, err := io.ReadFull(connection, methods); err != nil {
			return
		}
		if _, err := connection.Write([]byte{5, 0}); err != nil {
			return
		}

		requestHeader := make([]byte, 4)
		if _, err := io.ReadFull(connection, requestHeader); err != nil || requestHeader[0] != 5 || requestHeader[1] != 1 {
			return
		}
		var host string
		switch requestHeader[3] {
		case 1:
			address := make([]byte, net.IPv4len)
			if _, err := io.ReadFull(connection, address); err != nil {
				return
			}
			host = net.IP(address).String()
		case 4:
			address := make([]byte, net.IPv6len)
			if _, err := io.ReadFull(connection, address); err != nil {
				return
			}
			host = net.IP(address).String()
		case 3:
			length := make([]byte, 1)
			if _, err := io.ReadFull(connection, length); err != nil {
				return
			}
			address := make([]byte, int(length[0]))
			if _, err := io.ReadFull(connection, address); err != nil {
				return
			}
			host = string(address)
		default:
			return
		}
		portBytes := make([]byte, 2)
		if _, err := io.ReadFull(connection, portBytes); err != nil {
			return
		}
		target <- net.JoinHostPort(host, fmt.Sprint(binary.BigEndian.Uint16(portBytes)))

		originConnection, err := net.Dial("tcp", originAddress)
		if err != nil {
			return
		}
		defer originConnection.Close()
		if _, err := connection.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
			return
		}
		done := make(chan struct{}, 1)
		go func() {
			_, _ = io.Copy(originConnection, connection)
			done <- struct{}{}
		}()
		_, _ = io.Copy(connection, originConnection)
		<-done
	}()

	return videoArchiveProxyFixture{
		url:    scheme + "://" + listener.Addr().String(),
		target: target,
	}
}

func newVideoArchiveTestFetcher(t *testing.T, contentType string, body string) *HTTPVideoArchiveFetcher {
	t.Helper()
	return &HTTPVideoArchiveFetcher{
		client: &http.Client{Transport: videoArchiveRoundTripper(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{contentType}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    request,
			}, nil
		})},
		tempDir:        t.TempDir(),
		availableBytes: func(string) (uint64, error) { return 1 << 30, nil },
		validateURL: func(_ context.Context, parsed *url.URL) error {
			if parsed.Scheme != "https" {
				return ErrVideoArchiveSourceRejected
			}
			return nil
		},
	}
}
