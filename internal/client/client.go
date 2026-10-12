// Package client is a small SutramX REST API client authenticated with a
// workspace API key (Authorization: Bearer sk_...).
package client

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DefaultAPIURL = "https://api.sutramx.com"

const (
	maxAttempts = 4
	// A 429 is retried more often than other failures: the API refused the
	// request without running it, and a rate-limit window can be 15 minutes.
	maxRateLimitAttempts = 10
	// A create still running under the same Idempotency-Key (409
	// IDEMPOTENCY_REQUEST_IN_PROGRESS) is polled this many times at most.
	maxInProgressAttempts = 60
	// Longest single wait between attempts, also when Retry-After asks for more.
	maxRetryDelay = 120 * time.Second
)

// Error codes the API uses for plan limits (category PLAN_LIMIT on newer
// servers). Retrying never helps: the plan has to change first.
var planLimitCodes = map[string]bool{
	"ENTITLEMENT_LIMIT_REACHED":      true,
	"ENTITLEMENT_INTERVAL_TOO_SHORT": true,
	"MONITOR_TOTAL_LIMIT_REACHED":    true,
	"CONNECTION_LIMIT_REACHED":       true,
	"LIMIT_REACHED":                  true,
	"FEATURE_NOT_AVAILABLE":          true,
	"AI_LIMIT_REACHED":               true,
}

const idempotencyInProgressCode = "IDEMPOTENCY_REQUEST_IN_PROGRESS"

// idempotentPostRoutes are the POST routes the API runs at most once per
// Idempotency-Key. Mirrors IDEMPOTENT_ROUTES in
// backend/src/middleware/idempotency.ts (path after the optional /v1 prefix,
// no trailing slash): keep the two lists in step. Only these POSTs are
// retried; every other POST (acknowledge, resolve, run-check, pause, test,
// rotate-secret ...) is sent once, because the API would run it again.
var idempotentPostRoutes = []*regexp.Regexp{
	regexp.MustCompile(`^/monitors$`),
	regexp.MustCompile(`^/monitors/bulk$`),
	regexp.MustCompile(`^/monitors/detect/apply$`),
	regexp.MustCompile(`^/monitor-groups$`),
	regexp.MustCompile(`^/status/pages$`),
	regexp.MustCompile(`^/status/pages/[^/]+/monitors$`),
	regexp.MustCompile(`^/integrations/[^/]+/connections$`),
	regexp.MustCompile(`^/integrations/[^/]+/connect$`),
	regexp.MustCompile(`^/incidents$`),
	regexp.MustCompile(`^/incidents/runbooks$`),
	regexp.MustCompile(`^/incidents/[^/]+/notes$`),
	regexp.MustCompile(`^/maintenance$`),
	regexp.MustCompile(`^/automation/monitors/apply$`),
}

// safePostRoutes are POST routes that only read (nothing changes on the
// server), so a retry is safe without an Idempotency-Key. The API does not
// dedupe them: keep them out of idempotentPostRoutes.
var safePostRoutes = []*regexp.Regexp{
	regexp.MustCompile(`^/automation/monitors/plan$`),
}

// routeOf is path relative to the base URL without a query string, an /api
// or /v1 prefix and a trailing slash.
func routeOf(path string) string {
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	for _, prefix := range []string{"/api", "/v1"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			path = path[len(prefix):]
		}
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}

func matchesAny(routes []*regexp.Regexp, path string) bool {
	route := routeOf(path)
	for _, pattern := range routes {
		if pattern.MatchString(route) {
			return true
		}
	}
	return false
}

// isIdempotentPostRoute reports whether a POST to path is one the API
// dedupes by Idempotency-Key.
func isIdempotentPostRoute(path string) bool { return matchesAny(idempotentPostRoutes, path) }

// isSafePostRoute reports whether a POST to path only reads.
func isSafePostRoute(path string) bool { return matchesAny(safePostRoutes, path) }

// ValidateBaseURL checks an API base URL before any key is sent to it:
// https only (plain http just for loopback development servers), and no
// credentials, query or fragment. It returns the normalised URL.
func ValidateBaseURL(raw string) (string, error) {
	if raw == "" {
		return DefaultAPIURL, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("api_url is not a valid absolute URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("api_url must not contain credentials, a query string or a fragment")
	}
	host := strings.ToLower(u.Hostname())
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return "", fmt.Errorf("refusing to send the API key to %s://%s: api_url must use https:// (plain http is only allowed for localhost)", u.Scheme, u.Host)
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/"), nil
}

// IsOfficialHost reports whether a validated base URL points at sutramx.com
// (or loopback).
func IsOfficialHost(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "sutramx.com" || strings.HasSuffix(host, ".sutramx.com") || host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Error is a non-2xx answer from the API.
type Error struct {
	Status   int
	Code     string
	Category string
	Message  string
	// RetryAfter is the wait the API asked for (Retry-After), if any.
	RetryAfter time.Duration
}

// IsPlanLimit reports whether the API refused the request because of the
// workspace's plan (monitor count, interval, channel type...).
func (e *Error) IsPlanLimit() bool {
	return e.Category == "PLAN_LIMIT" || planLimitCodes[e.Code]
}

func (e *Error) Error() string {
	text := fmt.Sprintf("SutramX API error (HTTP %d): %s", e.Status, e.Message)
	if e.Code != "" {
		text = fmt.Sprintf("SutramX API error (HTTP %d %s): %s", e.Status, e.Code, e.Message)
	}
	if e.Status == http.StatusTooManyRequests && e.RetryAfter > 0 {
		text += fmt.Sprintf(" (the API asked to retry after %s)", e.RetryAfter.Round(time.Second))
	}
	return text
}

// AsPlanLimit returns the API error when err is a plan-limit refusal.
func AsPlanLimit(err error) (*Error, bool) {
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.IsPlanLimit() {
		return apiErr, true
	}
	return nil, false
}

// IsNotFound reports whether err is an API 404.
func IsNotFound(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

type Client struct {
	baseURL    string
	apiKey     string
	userAgent  string
	httpClient *http.Client
}

func New(apiKey, baseURL, userAgent string) *Client {
	if baseURL == "" {
		baseURL = DefaultAPIURL
	}
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		userAgent: userAgent,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
			// The API never redirects; a redirect must not carry the key elsewhere.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// parseRetryAfter reads a Retry-After header: delta-seconds or an HTTP-date.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		if seconds > int64(24*time.Hour/time.Second) {
			seconds = int64(24 * time.Hour / time.Second)
		}
		return time.Duration(seconds) * time.Second, true
	}
	if at, err := http.ParseTime(value); err == nil {
		return max(at.Sub(now), 0), true
	}
	return 0, false
}

// retryDelay honours Retry-After (seconds or HTTP-date, at most
// maxRetryDelay) or backs off exponentially with jitter.
func retryDelay(attempt int, retryAfter string) time.Duration {
	if wait, ok := parseRetryAfter(retryAfter, time.Now()); ok {
		return min(wait, maxRetryDelay)
	}
	return min(500*time.Millisecond<<min(attempt, 8)+rand.N(250*time.Millisecond), maxRetryDelay)
}

// fitsDeadline reports whether waiting d still leaves time before ctx's
// deadline (always true without one).
func fitsDeadline(ctx context.Context, d time.Duration) bool {
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) > d
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ParseError normalises the three error body shapes the backend uses:
// {"error": "msg", "code": "X"}, {"error": "Validation failed", "errors": [{field, message}]}
// and {"success": false, "error": {"code": "X", "message": "msg"}}.
func ParseError(status int, body []byte) *Error {
	out := &Error{Status: status, Message: http.StatusText(status)}
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		if text := strings.TrimSpace(string(body)); text != "" && len(text) < 300 {
			out.Message = text
		}
		return out
	}
	if errField, ok := raw["error"]; ok {
		var nested struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		}
		var message string
		if json.Unmarshal(errField, &message) == nil {
			out.Message = message
		} else if json.Unmarshal(errField, &nested) == nil {
			out.Message = nested.Message
			out.Code = nested.Code
			if len(nested.Details) > 0 {
				raw["details"] = nested.Details
			}
		}
	}
	if categoryField, ok := raw["category"]; ok {
		_ = json.Unmarshal(categoryField, &out.Category)
	}
	if codeField, ok := raw["code"]; ok {
		var code string
		if json.Unmarshal(codeField, &code) == nil && code != "" {
			out.Code = code
		}
	}
	if errorsField, ok := raw["errors"]; ok {
		var fieldErrors []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		}
		if json.Unmarshal(errorsField, &fieldErrors) == nil && len(fieldErrors) > 0 {
			parts := make([]string, 0, len(fieldErrors))
			for _, fe := range fieldErrors {
				if fe.Field != "" {
					parts = append(parts, fe.Field+": "+fe.Message)
				} else {
					parts = append(parts, fe.Message)
				}
			}
			out.Message = out.Message + ": " + strings.Join(parts, "; ")
		}
	}
	if detailsField, ok := raw["details"]; ok {
		var details struct {
			Specs []struct {
				Key    *string  `json:"key"`
				Errors []string `json:"errors"`
			} `json:"specs"`
		}
		if json.Unmarshal(detailsField, &details) == nil {
			for _, spec := range details.Specs {
				out.Message += "; " + strings.Join(spec.Errors, "; ")
			}
		}
	}
	return out
}

// Do sends a request; body (if not nil) is JSON-encoded and the JSON answer
// is decoded into out (if not nil). anonymous skips the API key.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any, anonymous bool) error {
	_, err := c.do(ctx, method, path, query, body, out, anonymous)
	return err
}

// Send is Do for writes: it also returns the `warnings` the API attached to
// the answer (e.g. "Ignored unknown field: foo"); older servers send none.
func (c *Client) Send(ctx context.Context, method, path string, body, out any) ([]string, error) {
	if method == http.MethodPost && body == nil {
		body = map[string]any{}
	}
	return c.do(ctx, method, path, nil, body, out, false)
}

// newIdempotencyKey is one random key per logical create POST, reused on
// every retry of it, so the API runs the create once (servers without
// Idempotency-Key support ignore the header).
func newIdempotencyKey() string {
	buf := make([]byte, 16)
	if _, err := cryptorand.Read(buf); err != nil {
		return ""
	}
	return "tf-" + hex.EncodeToString(buf)
}

// do sends the request with retries:
//
//   - 429 for every method (the API's rate limiters answer before the
//     handler, so the request did not run), up to maxRateLimitAttempts
//     times, waiting Retry-After (at most maxRetryDelay);
//   - 502/503/504 and network errors (timeouts included) for GET, PUT,
//     DELETE, the read-only POSTs in safePostRoutes and the POST creates in
//     idempotentPostRoutes, which carry an Idempotency-Key that makes the
//     API run them once;
//   - 409 IDEMPOTENCY_REQUEST_IN_PROGRESS (the first try of the same create
//     is still running) after Retry-After, with the same key.
//
// Any other POST (an action such as acknowledge, resolve or run-check) is
// sent once unless refused with 429: a retry could run it twice. 409 IDEMPOTENT_RESPONSE_WITHHELD is
// returned as an error.
//
// Plan-limit refusals are never retried. A wait that would run past ctx's
// deadline is not started; the API error is returned instead.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any, anonymous bool) ([]string, error) {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var encoded []byte
	if body != nil {
		var err error
		if encoded, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	// Only the creates the API dedupes carry a key: net/http's Transport
	// itself replays a request that has an Idempotency-Key header when a
	// reused connection drops, so an action must not have one.
	idempotencyKey := ""
	if method == http.MethodPost && isIdempotentPostRoute(path) {
		idempotencyKey = newIdempotencyKey()
	}
	dedupedPost := idempotencyKey != ""
	retrySafe := method == http.MethodGet || method == http.MethodPut || method == http.MethodDelete || dedupedPost ||
		(method == http.MethodPost && isSafePostRoute(path))
	failures, rateLimited, inProgress := 0, 0, 0
	for {
		var reader io.Reader
		if encoded != nil {
			reader = bytes.NewReader(encoded)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("X-SutramX-Client", c.userAgent)
		if encoded != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		if !anonymous {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			failures++
			if retrySafe && failures < maxAttempts && ctx.Err() == nil {
				delay := retryDelay(failures-1, "")
				if fitsDeadline(ctx, delay) {
					if waitErr := sleepCtx(ctx, delay); waitErr != nil {
						return nil, waitErr
					}
					continue
				}
			}
			return nil, fmt.Errorf("could not reach the SutramX API at %s: %w", c.baseURL, err)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		status := resp.StatusCode
		if status >= 200 && status <= 299 {
			if status == http.StatusNoContent || len(data) == 0 {
				return nil, nil
			}
			var envelope struct {
				Warnings []string `json:"warnings"`
			}
			_ = json.Unmarshal(data, &envelope)
			if out != nil {
				if err := json.Unmarshal(data, out); err != nil {
					return nil, fmt.Errorf("unexpected response from %s %s: %w", method, path, err)
				}
			}
			return envelope.Warnings, nil
		}

		apiErr := ParseError(status, data)
		retryAfter := resp.Header.Get("Retry-After")
		if wait, ok := parseRetryAfter(retryAfter, time.Now()); ok {
			apiErr.RetryAfter = wait
		}
		retry, attempt := false, 0
		switch {
		case apiErr.IsPlanLimit():
		case status == http.StatusTooManyRequests:
			rateLimited++
			retry, attempt = rateLimited < maxRateLimitAttempts, rateLimited-1
		case status == http.StatusConflict && apiErr.Code == idempotencyInProgressCode && dedupedPost:
			inProgress++
			retry = inProgress < maxInProgressAttempts
		case retrySafe && (status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout):
			failures++
			retry, attempt = failures < maxAttempts, failures-1
		}
		if retry {
			delay := retryDelay(attempt, retryAfter)
			if fitsDeadline(ctx, delay) {
				if waitErr := sleepCtx(ctx, delay); waitErr != nil {
					return nil, waitErr
				}
				continue
			}
		}
		return nil, apiErr
	}
}

func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, nil, out, false)
}

func (c *Client) GetPublic(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, nil, out, true)
}

func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	_, err := c.Send(ctx, http.MethodPost, path, body, out)
	return err
}

func (c *Client) Put(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPut, path, nil, body, out, false)
}

func (c *Client) Patch(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPatch, path, nil, body, out, false)
}

func (c *Client) Delete(ctx context.Context, path string) error {
	return c.Do(ctx, http.MethodDelete, path, nil, nil, nil, false)
}

// PathEscape escapes one path segment (monitor keys may contain "/").
func PathEscape(segment string) string {
	return url.PathEscape(segment)
}
