package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/sutramx/terraform-provider-sutramx/internal/client"
)

// configureClient extracts the API client for a resource, requiring a key.
func configureClient(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	data, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *providerData, got %T", req.ProviderData))
		return nil
	}
	if !data.hasKey {
		resp.Diagnostics.AddError("Missing SutramX API key",
			"Set api_key in the provider block or the SUTRAMX_API_KEY environment variable. Create a key in SutramX → Settings → API keys.")
		return nil
	}
	return data.client
}

// normalizeJSON re-encodes a JSON document with sorted keys and no
// whitespace, for semantic comparison.
func normalizeJSON(raw string) (string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	out, err := json.Marshal(value) // maps marshal with sorted keys
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// jsonEqual reports whether two JSON documents are semantically equal.
func jsonEqual(a, b string) bool {
	na, errA := normalizeJSON(a)
	nb, errB := normalizeJSON(b)
	return errA == nil && errB == nil && na == nb
}

// withoutKeys removes top-level keys from a JSON object (server-managed config).
func withoutKeys(raw json.RawMessage, keys ...string) string {
	if len(raw) == 0 {
		return "{}"
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return string(raw)
	}
	for _, key := range keys {
		delete(object, key)
	}
	out, _ := json.Marshal(object)
	return string(out)
}

// undeclaredKeys returns the keys that are not top-level keys of the JSON
// object raw (all of them when raw is not an object).
func undeclaredKeys(raw string, keys []string) []string {
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &object) != nil {
		return keys
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			out = append(out, key)
		}
	}
	return out
}

func randomKey(prefix string) string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return prefix + hex.EncodeToString(buf)
}

func stringOrNull(value *string) types.String {
	if value == nil || *value == "" {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

func stringListValue(ctx context.Context, values []string, diags *diag.Diagnostics) types.List {
	list, d := types.ListValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return list
}

func stringSetValue(ctx context.Context, values []string, diags *diag.Diagnostics) types.Set {
	if values == nil {
		values = []string{}
	}
	set, d := types.SetValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return set
}

func apiErrorDiag(diags *diag.Diagnostics, summary string, err error) {
	diags.AddError(summary, err.Error())
}
