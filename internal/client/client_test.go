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
