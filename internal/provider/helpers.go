package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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

// canonicalJSON is raw in the form Terraform's jsonencode() writes (object
// keys sorted at every level, no whitespace, <, > and & escaped), or raw
// unchanged when it is not valid JSON.
func canonicalJSON(raw string) string {
	normalized, err := normalizeJSON(raw)
	if err != nil {
		return raw
	}
	return normalized
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

// keepPriorKeys returns the remote config JSON object with each of keys
// copied from prior when the remote object lacks it (write-only values the
// API accepts but never returns).
func keepPriorKeys(remote, prior string, keys ...string) string {
	var remoteObject, priorObject map[string]json.RawMessage
	if json.Unmarshal([]byte(remote), &remoteObject) != nil || json.Unmarshal([]byte(prior), &priorObject) != nil || remoteObject == nil {
		return remote
	}
	changed := false
	for _, key := range keys {
		value, inPrior := priorObject[key]
		if _, inRemote := remoteObject[key]; inPrior && !inRemote {
			remoteObject[key] = value
			changed = true
		}
	}
	if !changed {
		return remote
	}
	out, err := json.Marshal(remoteObject)
	if err != nil {
		return remote
	}
	return string(out)
}

// keepPriorDNSSpelling returns the remote config of a DNS monitor with the
// configured hostname and record_type when they differ only in the way the
// API normalizes them (host name lower-cased without trailing dots, record
// type upper-cased, A when not set), so writing "Example.com." or "a", or
// leaving record_type out, is not a diff.
func keepPriorDNSSpelling(remote, prior string) string {
	var remoteObject, priorObject map[string]json.RawMessage
	if json.Unmarshal([]byte(remote), &remoteObject) != nil || json.Unmarshal([]byte(prior), &priorObject) != nil || remoteObject == nil {
		return remote
	}
	normalize := func(key, value string) string {
		value = strings.TrimSpace(value)
		if key == "hostname" {
			return strings.TrimRight(strings.ToLower(value), ".")
		}
		return strings.ToUpper(value)
	}
	changed := false
	// The API stores the default record type (A) when none is given.
	if _, configured := priorObject["record_type"]; !configured && string(remoteObject["record_type"]) == `"A"` {
		delete(remoteObject, "record_type")
		changed = true
	}
	for _, key := range []string{"hostname", "record_type"} {
		var remoteValue, priorValue string
		if json.Unmarshal(remoteObject[key], &remoteValue) != nil || json.Unmarshal(priorObject[key], &priorValue) != nil {
			continue
		}
		if remoteValue != priorValue && normalize(key, priorValue) == remoteValue {
			remoteObject[key] = priorObject[key]
			changed = true
		}
	}
	if !changed {
		return remote
	}
	out, err := json.Marshal(remoteObject)
	if err != nil {
		return remote
	}
	return string(out)
}

// tagValidator accepts a monitor tag in the form SutramX stores it: 1-32
// characters, lower-case, no surrounding whitespace. Anything else would be
// rewritten by the API and could never match the configuration.
var _ validator.String = tagValidator{}

type tagValidator struct{}

func (tagValidator) Description(_ context.Context) string {
	return "tags are 1-32 characters, lower-case, without surrounding spaces"
}

func (v tagValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (tagValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	tag := req.ConfigValue.ValueString()
	switch length := utf8.RuneCountInString(tag); {
	case strings.TrimSpace(tag) != tag:
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid tag", fmt.Sprintf("Tag %q has leading or trailing spaces; SutramX trims them, so write the tag without them.", tag))
	case length < 1 || length > 32:
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid tag", fmt.Sprintf("Tag %q must be 1-32 characters.", tag))
	case strings.ToLower(tag) != tag:
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid tag", fmt.Sprintf("Tag %q has upper-case letters; SutramX stores tags lower-case, so write it as %q.", tag, strings.ToLower(tag)))
	}
}

// isAPISpace reports whether JavaScript's String.prototype.trim and the \s
// regular expression class (which the API normalizes with) treat r as
// whitespace.
func isAPISpace(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' }

// trimAPISpace is value as the API's String.prototype.trim() leaves it.
func trimAPISpace(value string) string { return strings.TrimFunc(value, isAPISpace) }

// collapseAPISpace is value as the API's value.replace(/\s+/g, ' ').trim()
// leaves it (alert channel names).
func collapseAPISpace(value string) string {
	return strings.Join(strings.FieldsFunc(value, isAPISpace), " ")
}

// normalizedStringValidator accepts a string only in the form the API stores
// it: without surrounding whitespace (the API trims) and, with
// collapseSpaces, without runs of inner whitespace (the API collapses them
// to one space). Anything else would be read back changed and fail with
// "Provider produced inconsistent result after apply".
var _ validator.String = normalizedStringValidator{}

type normalizedStringValidator struct {
	// what names the attribute in messages, e.g. "Name".
	what           string
	collapseSpaces bool
}

func (v normalizedStringValidator) normalize(value string) string {
	if v.collapseSpaces {
		return collapseAPISpace(value)
	}
	return trimAPISpace(value)
}

func (v normalizedStringValidator) Description(_ context.Context) string {
	if v.collapseSpaces {
		return "must not have leading, trailing or repeated whitespace"
	}
	return "must not have leading or trailing whitespace"
}

func (v normalizedStringValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v normalizedStringValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	normalized := v.normalize(value)
	if normalized == value {
		return
	}
	if normalized == "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid "+strings.ToLower(v.what),
			fmt.Sprintf("%s is only whitespace; SutramX trims it to an empty value. Write a value or leave the attribute out.", v.what))
		return
	}
	how := "has leading or trailing whitespace; SutramX trims it"
	if v.collapseSpaces {
		how = "has leading, trailing or repeated whitespace; SutramX trims it and collapses inner whitespace to one space"
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Invalid "+strings.ToLower(v.what),
		fmt.Sprintf("%s %q %s, so write it as %q.", v.what, value, how, normalized))
}
