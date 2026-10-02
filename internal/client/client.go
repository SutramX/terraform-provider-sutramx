// Package client is a small SutramX REST API client authenticated with a
// workspace API key (Authorization: Bearer sk_...).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultAPIURL = "https://api.sutramx.com"

// Error is a non-2xx answer from the API.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("SutramX API error (HTTP %d %s): %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("SutramX API error (HTTP %d): %s", e.Status, e.Message)
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
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		userAgent:  userAgent,
		httpClient: &http.Client{Timeout: 60 * time.Second},
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
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if !anonymous {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the SutramX API at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return ParseError(resp.StatusCode, data)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("unexpected response from %s %s: %w", method, path, err)
	}
	return nil
}

func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, nil, out, false)
}

func (c *Client) GetPublic(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, nil, out, true)
}

func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	if body == nil {
		body = map[string]any{}
	}
	return c.Do(ctx, http.MethodPost, path, nil, body, out, false)
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
