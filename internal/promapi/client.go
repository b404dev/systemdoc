package promapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"systemdoc/internal/textutil"
)

const (
	// requestTimeout matches the ceiling commandcapture applies to CLI snapshots.
	requestTimeout = 8 * time.Second
	// bodyLimit matches the per-stream cap of commandcapture.
	bodyLimit = 4 << 20
	userAgent = "systemdoc"
)

// ErrUnauthorized reports a 401 or 403 from the server.
var ErrUnauthorized = errors.New("prometheus rejected the request; set SYSTEMDOC_PROMETHEUS_TOKEN or check the token")

// Client issues bounded requests to one Prometheus server.
type Client struct {
	base  *url.URL
	token string
	http  *http.Client
}

// New validates a base URL of the form http(s)://host[:port][/prefix] and
// returns a client. A missing scheme defaults to http. A username, password,
// query string or fragment is rejected so credentials cannot end up in
// settings or on screen; token is sent as a bearer header instead.
func New(raw, token string) (*Client, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("no Prometheus URL configured")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("prometheus URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("prometheus URL must use http or https, not %q", parsed.Scheme)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("prometheus URL must not contain a username or password; use SYSTEMDOC_PROMETHEUS_TOKEN")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("prometheus URL must not contain a query string or fragment")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("prometheus URL needs a host, for example http://localhost:9090")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &Client{
		base:  parsed,
		token: token,
		http: &http.Client{
			Timeout:   requestTimeout,
			Transport: transport,
			// A redirect could carry the bearer token to another host; the
			// user configures the final URL instead.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// String is the base URL for status lines. It never carries credentials.
func (c *Client) String() string { return c.base.String() }

// endpoint joins the API path under any configured prefix.
func (c *Client) endpoint(apiPath string) string {
	u := *c.base
	u.Path = path.Join(c.base.Path, apiPath)
	return u.String()
}

// fetch issues one bounded request and returns the body. Errors name the API
// path and the base URL only, never the token.
func (c *Client) fetch(ctx context.Context, method, apiPath string, form url.Values) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	target := c.endpoint(apiPath)
	var body io.Reader
	if method == http.MethodGet && len(form) > 0 {
		target += "?" + form.Encode()
	} else if len(form) > 0 {
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return 0, nil, fmt.Errorf("%s: %w", apiPath, err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", userAgent)
	if body != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, fmt.Errorf("%s: %w", apiPath, ctx.Err())
		}
		return 0, nil, fmt.Errorf("%s at %s: %s", apiPath, c.String(), transportReason(err))
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, bodyLimit+1))
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, fmt.Errorf("%s: %w", apiPath, ctx.Err())
		}
		return 0, nil, fmt.Errorf("%s: reading response: %w", apiPath, err)
	}
	if len(data) > bodyLimit {
		return 0, nil, fmt.Errorf("%s: response exceeded 4 MiB; it was not used", apiPath)
	}
	return response.StatusCode, data, nil
}

// transportReason strips the request URL from a net/http error so a status
// line shows the cause rather than a long address.
func transportReason(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return textutil.Clean(urlErr.Err.Error())
	}
	return textutil.Clean(err.Error())
}

// do fetches an /api/v1 endpoint and decodes its envelope into out.
func (c *Client) do(ctx context.Context, method, apiPath string, form url.Values, out any) error {
	status, data, err := c.fetch(ctx, method, apiPath, form)
	if err != nil {
		return err
	}
	switch {
	case status >= 300 && status < 400:
		return fmt.Errorf("%s: redirected by %s; configure the final Prometheus URL", apiPath, c.String())
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Errorf("%s: %w", apiPath, ErrUnauthorized)
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Status == "" {
		// The body is not echoed: an upstream proxy may reflect request headers.
		if status >= 400 {
			return fmt.Errorf("%s: HTTP %d from %s", apiPath, status, c.String())
		}
		return fmt.Errorf("%s: not a Prometheus API response from %s", apiPath, c.String())
	}
	if env.Status != "success" {
		return fmt.Errorf("%s: %s: %s", apiPath, textutil.Clean(env.ErrorType), firstLine(textutil.Clean(env.Error)))
	}
	if status >= 400 {
		return fmt.Errorf("%s: HTTP %d", apiPath, status)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("%s: unexpected response shape: %w", apiPath, err)
	}
	return nil
}
