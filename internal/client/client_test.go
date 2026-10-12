package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseErrorShapes(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		code    string
		message string
	}{
		{"flat", `{"error":"Monitor not found","code":"NOT_FOUND"}`, "NOT_FOUND", "Monitor not found"},
		{"nested", `{"success":false,"error":{"code":"FEATURE_NOT_AVAILABLE","message":"Upgrade"}}`, "FEATURE_NOT_AVAILABLE", "Upgrade"},
		{"validation", `{"error":"Validation failed","errors":[{"field":"name","message":"Required"}]}`, "", "Validation failed: name: Required"},
		{"specs", `{"success":false,"error":{"code":"INVALID_MONITOR_SPEC","message":"1 monitor spec is invalid","details":{"specs":[{"key":"a","errors":["interval_seconds: too small"]}]}}}`, "INVALID_MONITOR_SPEC", "1 monitor spec is invalid; interval_seconds: too small"},
		{"text", `Bad gateway`, "", "Bad gateway"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ParseError(400, []byte(tc.body))
			if err.Code != tc.code || err.Message != tc.message {
				t.Fatalf("got code=%q message=%q", err.Code, err.Message)
			}
		})
	}
}

func TestDoSendsKeyAndDecodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/catalog/regions" {
			if r.Header.Get("Authorization") != "" {
				t.Errorf("public endpoint must not receive the key")
			}
			_ = json.NewEncoder(w).Encode(RegionsResponse{Regions: []Region{{Code: "bom", Name: "Mumbai", Online: true}}})
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk_test" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Invalid API key"}`))
			return
		}
		if r.URL.EscapedPath() != "/automation/monitors/web%2Fhome" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"Monitor not found","code":"NOT_FOUND"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(Monitor{ID: "m1", Name: "Home"})
	}))
	defer server.Close()

	c := New("sk_test", server.URL, "test")
	var monitor Monitor
	if err := c.Get(context.Background(), "/automation/monitors/"+PathEscape("web/home"), &monitor); err != nil {
		t.Fatal(err)
	}
	if monitor.ID != "m1" {
		t.Fatalf("decoded %+v", monitor)
	}
	if err := c.Get(context.Background(), "/monitors/missing", &monitor); !IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
	var regions RegionsResponse
	if err := c.GetPublic(context.Background(), "/catalog/regions", &regions); err != nil || len(regions.Regions) != 1 {
		t.Fatalf("regions %v %v", regions, err)
	}
	bad := New("sk_wrong", server.URL, "test")
	if err := bad.Get(context.Background(), "/monitors", nil); err == nil || err.(*Error).Status != 401 {
		t.Fatalf("expected 401, got %v", err)
	}
}

func TestValidateBaseURL(t *testing.T) {
	ok := map[string]string{
		"":                          DefaultAPIURL,
		"https://api.sutramx.com/":  "https://api.sutramx.com",
		"http://localhost:3001":     "http://localhost:3001",
		"http://127.0.0.1:3001/api": "http://127.0.0.1:3001/api",
	}
	for in, want := range ok {
		got, err := ValidateBaseURL(in)
		if err != nil || got != want {
			t.Errorf("ValidateBaseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"http://api.sutramx.com", "ftp://example.com", "https://u:p@example.com", "https://example.com/?x=1", "not a url", "//example.com"} {
		if _, err := ValidateBaseURL(in); err == nil {
			t.Errorf("ValidateBaseURL(%q) should fail", in)
		}
	}
	if !IsOfficialHost("https://api.sutramx.com") || IsOfficialHost("https://sutramx.com.evil.io") {
		t.Error("IsOfficialHost")
	}
}

func TestDoRetries429AndNotPost500(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch {
		case r.URL.Path == "/limited" && hits < 3:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow down"}`))
		case r.URL.Path == "/boom":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		case r.URL.Path == "/redirect":
			http.Redirect(w, r, "https://example.com/steal", http.StatusFound)
		default:
			_, _ = w.Write([]byte(`{"id":"m1"}`))
		}
	}))
	defer server.Close()
	c := New("sk_test", server.URL, "test")
	var monitor Monitor
	if err := c.Get(context.Background(), "/limited", &monitor); err != nil || monitor.ID != "m1" || hits != 3 {
		t.Fatalf("429 retry: err=%v hits=%d", err, hits)
	}
	hits = 0
	if err := c.Post(context.Background(), "/boom", nil, nil); err == nil || hits != 1 {
		t.Fatalf("POST 500 must not be retried: err=%v hits=%d", err, hits)
	}
	if err := c.Get(context.Background(), "/redirect", &monitor); err == nil {
		t.Fatal("redirects must not be followed")
	}
}

func TestPostSendsOneIdempotencyKeyAcrossRetries(t *testing.T) {
	var keys []string
	var clients []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		clients = append(clients, r.Header.Get("X-SutramX-Client")+"|"+r.Header.Get("User-Agent"))
		switch len(keys) {
		case 1:
			w.WriteHeader(http.StatusBadGateway)
		case 2:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"A request with this key is still running","code":"IDEMPOTENCY_REQUEST_IN_PROGRESS"}`))
		case 3:
			// Network failure mid-request: the connection is dropped.
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		default:
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"p1","warnings":["Ignored unknown field: foo"]}`))
		}
	}))
	defer server.Close()
	c := New("sk_test", server.URL, "terraform-provider-sutramx/1.2.3")
	var page StatusPage
	warnings, err := c.Send(context.Background(), http.MethodPost, "/status/pages", map[string]any{"title": "x"}, &page)
	if err != nil || page.ID != "p1" {
		t.Fatalf("err=%v page=%+v", err, page)
	}
	if len(keys) != 4 || keys[0] == "" || keys[1] != keys[0] || keys[2] != keys[0] || keys[3] != keys[0] {
		t.Fatalf("one key for every retry of the same POST, got %q", keys)
	}
	if clients[0] != "terraform-provider-sutramx/1.2.3|terraform-provider-sutramx/1.2.3" {
		t.Fatalf("client headers %q", clients[0])
	}
	if len(warnings) != 1 || warnings[0] != "Ignored unknown field: foo" {
		t.Fatalf("warnings %q", warnings)
	}

	// A second logical create gets a new key.
	if err := c.Post(context.Background(), "/status/pages", nil, nil); err != nil {
		t.Fatal(err)
	}
	if keys[4] == keys[0] {
		t.Fatal("each POST needs its own Idempotency-Key")
	}
	// Reads and updates carry no key.
	_ = c.Get(context.Background(), "/status/pages/p1", nil)
	if keys[5] != "" {
		t.Fatalf("GET sent Idempotency-Key %q", keys[5])
	}
}

func TestPlanLimitIsNotRetried(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Retry-After", "0")
		if r.URL.Path == "/category" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"Monthly AI limit reached","code":"SOMETHING_NEW","category":"PLAN_LIMIT","details":{"resource":"ai"}}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"Your plan allows 10 monitors","code":"ENTITLEMENT_LIMIT_REACHED"}`))
	}))
	defer server.Close()
	c := New("sk_test", server.URL, "test")
	for _, path := range []string{"/category", "/code"} {
		hits = 0
		err := c.Post(context.Background(), path, nil, nil)
		if apiErr, ok := AsPlanLimit(err); !ok || hits != 1 {
			t.Fatalf("%s: plan limit must fail at once: err=%v (%+v) hits=%d", path, err, apiErr, hits)
		}
	}
	if _, ok := AsPlanLimit(&Error{Status: 403, Code: "READ_ONLY_ACCESS"}); ok {
		t.Fatal("READ_ONLY_ACCESS is not a plan limit")
	}
}

func TestDeleteRetries429WithRetryAfter(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits <= 6 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"Too many requests","code":"RATE_LIMITED"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	c := New("sk_test", server.URL, "test")
	// More 429s in a row than the 4 attempts other failures get.
	if err := c.Delete(context.Background(), "/monitors/m1"); err != nil || hits != 7 {
		t.Fatalf("err=%v hits=%d", err, hits)
	}
}

func TestRetryAfterBeyondDeadlineFailsFast(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"Too many requests","code":"RATE_LIMITED"}`))
	}))
	defer server.Close()
	c := New("sk_test", server.URL, "test")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	err := c.Delete(ctx, "/monitors/m1")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 429 || apiErr.RetryAfter != 90*time.Second || hits != 1 {
		t.Fatalf("err=%v hits=%d", err, hits)
	}
	if time.Since(started) > time.Second {
		t.Fatal("must not wait past the deadline")
	}
	if !strings.Contains(err.Error(), "retry after 1m30s") {
		t.Fatalf("message should mention Retry-After: %v", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	cases := map[string]time.Duration{
		"0":                             0,
		"45":                            45 * time.Second,
		" 600 ":                         600 * time.Second,
		"Mon, 12 Oct 2026 10:01:30 GMT": 90 * time.Second,
		"Mon, 12 Oct 2026 09:59:00 GMT": 0,
	}
	for in, want := range cases {
		got, ok := parseRetryAfter(in, now)
		if !ok || got != want {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "-1", "soon", "1.5"} {
		if _, ok := parseRetryAfter(in, now); ok {
			t.Errorf("parseRetryAfter(%q) should be ignored", in)
		}
	}
	if d := retryDelay(0, "600"); d != maxRetryDelay || maxRetryDelay != 120*time.Second {
		t.Fatalf("long Retry-After capped at 120s, got %v", d)
	}
	if d := retryDelay(0, "100"); d != 100*time.Second {
		t.Fatalf("Retry-After under the cap is honoured, got %v", d)
	}
	if d := retryDelay(30, ""); d > maxRetryDelay {
		t.Fatalf("backoff capped, got %v", d)
	}
}

func TestParseErrorCategory(t *testing.T) {
	err := ParseError(403, []byte(`{"error":"Upgrade","code":"FEATURE_NOT_AVAILABLE","category":"PLAN_LIMIT","details":{"resource":"monitors"}}`))
	if err.Category != "PLAN_LIMIT" || !err.IsPlanLimit() || err.Message != "Upgrade" {
		t.Fatalf("%+v", err)
	}
}

func TestIdempotentPostRoutes(t *testing.T) {
	for _, path := range []string{"/monitors", "/v1/monitors", "/monitors/", "/monitors?x=1", "/monitors/bulk", "/monitors/detect/apply", "/monitor-groups",
		"/status/pages", "/status/pages/p1/monitors", "/integrations/slack/connections", "/integrations/slack/connect", "/incidents",
		"/incidents/runbooks", "/incidents/i1/notes", "/maintenance", "/automation/monitors/apply", "/api/v1/maintenance"} {
		if !isIdempotentPostRoute(path) {
			t.Errorf("%s should be retried", path)
		}
	}
	for _, path := range []string{"/incidents/i1/acknowledge", "/incidents/i1/resolve", "/monitors/m1/run-check", "/monitors/m1/pause",
		"/monitors/m1/resume", "/integrations/slack/connections/c1/test", "/monitors/m1/heartbeat/rotate-secret", "/automation/monitors/plan", "/monitors/m1"} {
		if isIdempotentPostRoute(path) {
			t.Errorf("%s must not be retried", path)
		}
	}
}

func TestNonIdempotentPostIsSentOnce(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if key := r.Header.Get("Idempotency-Key"); key != "" {
			// net/http would replay such a request on a dropped reused connection.
			t.Errorf("%s carried Idempotency-Key %q", r.URL.Path, key)
		}
		switch r.URL.Path {
		case "/incidents/i1/acknowledge":
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		case "/incidents/i1/resolve/slow":
			time.Sleep(500 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		case "/incidents/i2/resolve":
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"running","code":"IDEMPOTENCY_REQUEST_IN_PROGRESS"}`))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	c := New("sk_test", server.URL, "test")
	ctx := context.Background()
	for _, path := range []string{"/incidents/i1/resolve", "/monitors/m1/pause", "/integrations/slack/connections/c1/test", "/monitors/m1/run-check", "/incidents/i1/acknowledge", "/incidents/i2/resolve"} {
		paths = nil
		if err := c.Post(ctx, path, nil, nil); err == nil {
			t.Fatalf("%s: expected an error", path)
		}
		if len(paths) != 1 {
			t.Fatalf("%s: attempted %d times, want once", path, len(paths))
		}
	}
	// A timeout is not retried either.
	c.httpClient.Timeout = 100 * time.Millisecond
	paths = nil
	if err := c.Post(ctx, "/incidents/i1/resolve/slow", nil, nil); err == nil {
		t.Fatal("expected a timeout")
	}
	time.Sleep(600 * time.Millisecond)
	if len(paths) != 1 {
		t.Fatalf("timed-out POST attempted %d times, want once", len(paths))
	}
}

func TestCreatePostRetriedWithSameKey(t *testing.T) {
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		switch len(keys) {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			time.Sleep(300 * time.Millisecond) // times out
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"m1"}`))
		}
	}))
	defer server.Close()
	c := New("sk_test", server.URL+"/v1", "test")
	c.httpClient.Timeout = 100 * time.Millisecond
	var monitor Monitor
	if err := c.Post(context.Background(), "/monitors", map[string]any{"name": "x"}, &monitor); err != nil || monitor.ID != "m1" {
		t.Fatalf("err=%v monitor=%+v", err, monitor)
	}
	if len(keys) != 3 || keys[0] == "" || keys[1] != keys[0] || keys[2] != keys[0] {
		t.Fatalf("POST /monitors must be retried with one key, got %q", keys)
	}

	// 409 IDEMPOTENT_RESPONSE_WITHHELD is an error, not retried.
	hits := 0
	withheld := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"The original response held a secret","code":"IDEMPOTENT_RESPONSE_WITHHELD"}`))
	}))
	defer withheld.Close()
	err := New("sk_test", withheld.URL, "test").Post(context.Background(), "/monitors", nil, nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "IDEMPOTENT_RESPONSE_WITHHELD" || hits != 1 {
		t.Fatalf("withheld: err=%v hits=%d", err, hits)
	}
}

func TestActionPostRetriedOnlyOn429AndPlanLikeARead(t *testing.T) {
	if !isSafePostRoute("/automation/monitors/plan") || !isSafePostRoute("/v1/automation/monitors/plan/") || isSafePostRoute("/automation/monitors/apply") || isSafePostRoute("/incidents/i1/resolve") {
		t.Fatal("safePostRoutes")
	}
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if len(paths) == 1 {
			if r.URL.Path == "/monitors/m1/run-check" {
				// A rate limiter answers before the handler: the check did not run.
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"slow down"}`))
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	c := New("sk_test", server.URL, "test")
	ctx := context.Background()
	if err := c.Post(ctx, "/monitors/m1/run-check", nil, nil); err != nil || len(paths) != 2 {
		t.Fatalf("429 on an action is retried: err=%v paths=%q", err, paths)
	}
	paths = nil
	if err := c.Post(ctx, "/automation/monitors/plan", nil, nil); err != nil || len(paths) != 2 {
		t.Fatalf("the read-only plan is retried on 503: err=%v paths=%q", err, paths)
	}
}
