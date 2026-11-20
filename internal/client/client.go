// Package client is the thin HTTP client the yottactl CLI uses to reach
// the platform REST API. Plan #60 Track B B1.
//
// Every call carries Authorization: Bearer <token> + an
// X-Yotta-Client-Version header (so the platform can return a
// deprecation hint when the CLI is too old, per plan #60 Track D
// risk note). The client is deliberately minimal — it returns the
// raw status + body and lets the output layer render; it does no
// response typing (the CLI is schema-agnostic, mirroring kubectl's
// raw-passthrough for unknown resources).
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Version is stamped into X-Yotta-Client-Version. Overridden at
// build time via -ldflags in CI; defaults to "dev".
var Version = "dev"

type RawBody struct {
	Data        []byte
	ContentType string
}

type Client struct {
	endpoint string
	token    string
	http     *http.Client
	query    url.Values
	headers  map[string]string
}

func New(endpoint, token string) *Client {
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		token:    token,
		http: &http.Client{Timeout: 30 * time.Second,
			// An API redirect is a response, not permission to replay a token
			// or a mutating body at a different destination.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// SetQuery adds explicit query parameters to API calls. Values are encoded by
// net/url, never concatenated into a path or interpreted as another flag.
func (c *Client) SetQuery(query url.Values)            { c.query = query }
func (c *Client) SetHeaders(headers map[string]string) { c.headers = headers }

// Do issues a request. body is JSON-marshalled when non-nil. Returns
// the HTTP status, the raw response body, and a transport error (nil
// for any HTTP status — the caller maps status to exit code).
func (c *Client) Do(method, path string, body any) (int, []byte, error) {
	return c.DoWithHeaders(method, path, body, nil)
}

// DoWithHeaders is the operator-workflow variant of Do. It is intentionally
// narrow: callers provide a fixed map assembled by command code (for example
// Idempotency-Key), while Authorization and the client version remain owned by
// this client and cannot be overridden.
func (c *Client) DoWithHeaders(method, path string, body any, headers map[string]string) (int, []byte, error) {
	resp, err := c.DoResponse(method, path, body, headers)
	return resp.Status, resp.Body, err
}

// Response is one exchange's outcome including its headers.
type Response struct {
	Status int
	Body   []byte
	// Header is a clone: the caller owns it.
	Header http.Header
}

// DoResponse is DoWithHeaders returning the response headers too (plan #244
// WP2b-3). The canonical workflow routes are conditional — a draft write needs
// the ETag its read returned — and a caller cannot send an If-Match it was
// never shown. Do and DoWithHeaders keep their signatures; this is additive.
func (c *Client) DoResponse(method, path string, body any, headers map[string]string) (Response, error) {
	if c.endpoint == "" {
		return Response{}, fmt.Errorf("no endpoint configured — set one with `yottactl config set-context` or --endpoint")
	}
	var rdr io.Reader
	contentType := "application/json"
	if raw, ok := body.(RawBody); ok {
		rdr = bytes.NewReader(raw.Data)
		contentType = raw.ContentType
	} else if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return Response{}, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.endpoint+path, rdr)
	if err != nil {
		return Response{}, err
	}
	for _, segment := range strings.Split(req.URL.EscapedPath(), "/") {
		if segment == "." || segment == ".." {
			return Response{}, fmt.Errorf("path contains a traversal segment")
		}
	}
	q := req.URL.Query()
	for k, values := range c.query {
		q[k] = values
	}
	req.URL.RawQuery = q.Encode()
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("X-Yotta-Client-Version", Version)
	req.Header.Set("X-Yotta-Client", clientName())
	combined := map[string]string{}
	for name, value := range c.headers {
		combined[http.CanonicalHeaderKey(name)] = value
	}
	for name, value := range headers {
		combined[http.CanonicalHeaderKey(name)] = value
	}
	for name, value := range combined {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "X-Yotta-Client-Version") {
			continue
		}
		req.Header.Set(name, value)
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	out := Response{Status: resp.StatusCode, Header: resp.Header.Clone()}
	out.Body, err = io.ReadAll(resp.Body)
	if err != nil {
		return Response{Status: resp.StatusCode, Header: out.Header}, err
	}
	return out, nil
}

// clientName is what a request calls its client: yottactl, or who is running it
// (Albus sets YOTTA_CLIENT). Services record it as a claim, never as proof.
func clientName() string {
	if name := strings.TrimSpace(os.Getenv("YOTTA_CLIENT")); name != "" {
		return name
	}
	return "yottactl"
}
