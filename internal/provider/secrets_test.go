package provider

import "testing"

func TestKeepPriorSecretsRestoresMaskedValues(t *testing.T) {
	prior := `{"timeout":10000,"headers":{"Authorization":"Bearer live","X-Api-Key":12345,"Accept":"application/json"},"basic_auth":{"password":"pw"},"url_list":["https://u:p@a.example/x"]}`
	remote := `{"headers":{"Accept":"application/json","Authorization":"[REDACTED]","X-Api-Key":"[REDACTED]"},"timeout":10000,"basic_auth":{"password":"[REDACTED]"},"url_list":["https://u:[REDACTED]@a.example/x"]}`
	got := keepPriorSecrets(remote, prior)
	if !jsonEqual(got, prior) {
		t.Fatalf("masked values were not restored from prior:\n got  %s\n want %s", got, prior)
	}
}

func TestKeepPriorSecretsKeepsRealChangesAndUnknownMasks(t *testing.T) {
	prior := `{"timeout":10000,"headers":{"Authorization":"Bearer live"}}`
	// A non-secret change outside Terraform still shows; a masked header with
	// no configured counterpart stays masked (shows as a diff).
	remote := `{"timeout":5000,"headers":{"Authorization":"[REDACTED]","Cookie":"[REDACTED]"}}`
	want := `{"timeout":5000,"headers":{"Authorization":"Bearer live","Cookie":"[REDACTED]"}}`
	if got := keepPriorSecrets(remote, prior); !jsonEqual(got, want) {
		t.Fatalf("got %s, want %s", got, want)
	}
	// Nothing masked: returned unchanged.
	if got := keepPriorSecrets(`{"a":1}`, prior); got != `{"a":1}` {
		t.Fatalf("unmasked remote changed: %s", got)
	}
	// A masked URL on another host does not take the prior value.
	remoteURL := `{"target":"https://u:[REDACTED]@evil.example/x"}`
	if got := keepPriorSecrets(remoteURL, `{"target":"https://u:p@a.example/x"}`); !jsonEqual(got, remoteURL) {
		t.Fatalf("masked URL on another host was restored: %s", got)
	}
}

func TestKeepPriorURL(t *testing.T) {
	cases := []struct {
		prior, remote string
		keep          bool
	}{
		{"https://Example.com/health", "https://example.com/health", true},
		{"https://svc:pw@api.example.com/health", "https://svc:[REDACTED]@api.example.com/health", true},
		{"https://token@api.example.com/health", "https://[REDACTED]@api.example.com/health", true},
		{"https://svc:pw@api.example.com/health", "https://svc:[REDACTED]@other.example.com/health", false},
		{"https://api.example.com/health", "https://api.example.com/v2", false},
	}
	for _, tc := range cases {
		if got := keepPriorURL(tc.prior, tc.remote); got != tc.keep {
			t.Errorf("keepPriorURL(%q, %q) = %v, want %v", tc.prior, tc.remote, got, tc.keep)
		}
	}
}
