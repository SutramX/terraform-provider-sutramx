package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
