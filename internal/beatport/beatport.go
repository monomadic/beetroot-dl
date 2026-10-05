package beatport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	beatportBaseUrl = "https://api.beatport.com/v4"
)

type Beatport struct {
	client         *http.Client
	downloadClient *http.Client
	headers        map[string]string
	auth           *Auth
	// baseUrl is the API root. It is always beatportBaseUrl in production;
	// tests point it at an httptest server.
	baseUrl     string
	ctx         context.Context
	diagnostics io.Writer
}

type FetcherError struct {
	Detail *string `json:"detail,omitempty"`
	Error  *string `json:"error,omitempty"`
}

type Paginated[T any] struct {
	Next     *string `json:"next"`
	Previous *string `json:"previous"`
	Count    int     `json:"count"`
	Page     string  `json:"page"`
	PerPage  int     `json:"per_page"`
	Results  []T     `json:"results"`
	Facets   Facets  `json:"facets"`
}

type Facets struct {
	Fields map[string][]Facet `json:"fields"`
}

type Facet struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// New builds the API client. An invalid proxy URL is an error rather than a
// silent fallback to a direct connection: someone who configured a proxy would
// not want their traffic quietly going out unproxied.
func New(proxyUrl string, auth *Auth) (*Beatport, error) {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 40 * time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
	}
	if proxyUrl != "" {
		parsed, err := url.Parse(proxyUrl)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy url %q: %w", proxyUrl, err)
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	headers := map[string]string{
		"accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"accept-language": "en-US,en;q=0.9",
		"cache-control":   "max-age=0",
		"user-agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
	}
	f := Beatport{
		auth: auth,
		client: &http.Client{
			Timeout:   time.Duration(40) * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		// Media transfers share the transport — and therefore the proxy — but
		// must not inherit the API client's 40s whole-request deadline, which
		// a large FLAC would routinely exceed. The transport's dial and
		// response-header timeouts still bound a stalled connection, and
		// callers pass a context for cancellation.
		downloadClient: &http.Client{Transport: transport},
		headers:        headers,
		baseUrl:        beatportBaseUrl,
	}
	return &f, nil
}

// DownloadClient is the client to use for track, cover and HLS-segment
// transfers. Using it (rather than http.DefaultClient) is what keeps those
// requests on the configured proxy.
func (b *Beatport) DownloadClient() *http.Client {
	return b.downloadClient
}

// WithContext shares the connection pool and authentication, while binding
// all API calls (including refresh/login) to one operation's lifetime.
func (b *Beatport) WithContext(ctx context.Context) *Beatport {
	copy := *b
	copy.ctx = ctx
	return &copy
}

func (b *Beatport) WithDiagnostics(w io.Writer) *Beatport {
	copy := *b
	copy.diagnostics = w
	return &copy
}

func (b *Beatport) log(message string) {
	w := b.diagnostics
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintln(w, message)
}

// fetch performs an authenticated request. A 401 triggers one re-auth and one
// retry; a second 401 is returned as an error rather than retried, so a
// persistently rejecting endpoint cannot spin forever re-logging-in.
func (b *Beatport) fetch(method, endpoint string, payload interface{}, contentType string) (*http.Response, error) {
	return b.fetchWithRetry(method, endpoint, payload, contentType, 1)
}

func (b *Beatport) fetchWithRetry(method, endpoint string, payload interface{}, contentType string, retriesLeft int) (*http.Response, error) {
	var body bytes.Buffer
	ctx := b.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if endpoint != tokenEndpoint && endpoint != authEndpoint && endpoint != loginEndpoint {
		if err := b.auth.Check(b); err != nil {
			return nil, err
		}
	}

	if payload != nil {
		switch contentType {
		case "application/json":
			if err := json.NewEncoder(&body).Encode(payload); err != nil {
				return nil, fmt.Errorf("failed to encode json payload: %w", err)
			}
		case "application/x-www-form-urlencoded":
			formData, err := encodeFormPayload(payload)
			if err != nil {
				return nil, fmt.Errorf("failed to encode form payload: %w", err)
			}
			body.WriteString(formData.Encode())
		default:
			return nil, fmt.Errorf("unsupported content type: %s", contentType)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, b.baseUrl+endpoint, &body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for key, value := range b.headers {
		req.Header.Add(key, value)
	}

	if payload != nil {
		req.Header.Set("Content-Type", contentType)
	}

	if token := b.auth.AccessToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		if resp.StatusCode == http.StatusUnauthorized && retriesLeft > 0 &&
			endpoint != tokenEndpoint && endpoint != authEndpoint && endpoint != loginEndpoint {
			resp.Body.Close()
			b.auth.Invalidate()
			return b.fetchWithRetry(method, endpoint, payload, contentType, retriesLeft-1)
		}
		defer resp.Body.Close()
		response := &FetcherError{}
		if err = json.NewDecoder(resp.Body).Decode(response); err == nil {
			detail := "Unknown error"
			if response.Detail != nil {
				detail = *response.Detail
			} else if response.Error != nil {
				detail = *response.Error
			}
			return nil, fmt.Errorf(
				"request failed with status code: %d - %s",
				resp.StatusCode,
				detail,
			)
		}
		return nil, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	return resp, nil
}

func encodeFormPayload(payload interface{}) (url.Values, error) {
	values := url.Values{}

	switch p := payload.(type) {
	case map[string]string:
		for key, value := range p {
			values.Set(key, value)
		}
	case url.Values:
		values = p
	default:
		return nil, errors.New("invalid payload")
	}

	return values, nil
}
