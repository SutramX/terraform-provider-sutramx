package provider

import (
	"encoding/json"
	"strings"
)

// maskedSecret is what the API returns instead of a stored monitor credential
// (sensitive request header values, tokens, passwords, user:password@ in
// URLs). Sending it back keeps the stored value; the provider never needs to,
// because it always sends the configuration's real values.
const maskedSecret = "[REDACTED]"

// encodedMaskedSecret is maskedSecret after URL percent-encoding (userinfo).
const encodedMaskedSecret = "%5BREDACTED%5D"

func isMaskedSecret(value string) bool {
	return strings.Contains(value, maskedSecret) || strings.Contains(strings.ToUpper(value), encodedMaskedSecret)
}

// keepPriorSecrets returns the remote config JSON with every masked value
// replaced by the value at the same path in prior (the configuration or the
// state), so a configuration holding the real secret does not show a
// perpetual diff against the masked read-back. Masked values with no
// counterpart in prior stay masked. A secret changed outside Terraform
// cannot be detected (the API never reveals it).
func keepPriorSecrets(remote, prior string) string {
	if !isMaskedSecret(remote) || prior == "" {
		return remote
	}
	remoteValue, errRemote := decodeJSONValue(remote)
	priorValue, errPrior := decodeJSONValue(prior)
	if errRemote != nil || errPrior != nil {
		return remote
	}
	out, err := json.Marshal(mergeMaskedValues(remoteValue, priorValue))
	if err != nil {
		return remote
	}
	return string(out)
}

func decodeJSONValue(raw string) (any, error) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func mergeMaskedValues(remote, prior any) any {
	switch value := remote.(type) {
	case string:
		if !isMaskedSecret(value) || prior == nil {
			return value
		}
		switch priorValue := prior.(type) {
		case string:
			if value == maskedSecret || sameIgnoringURLUserinfo(value, priorValue) {
				return priorValue
			}
		case json.Number, bool:
			// A masked number (e.g. a numeric API key header).
			if value == maskedSecret {
				return priorValue
			}
		}
		return value
	case map[string]any:
		priorMap, _ := prior.(map[string]any)
		out := make(map[string]any, len(value))
		for key, item := range value {
			var priorItem any
			if priorMap != nil {
				priorItem = priorMap[key]
			}
			out[key] = mergeMaskedValues(item, priorItem)
		}
		return out
	case []any:
		priorList, _ := prior.([]any)
		out := make([]any, len(value))
		for index, item := range value {
			var priorItem any
			if index < len(priorList) {
				priorItem = priorList[index]
			}
			out[index] = mergeMaskedValues(item, priorItem)
		}
		return out
	default:
		return remote
	}
}

// stripURLUserinfo removes user:password@ from the first URL authority in
// raw (string-level: net/url rejects the masked "[REDACTED]" userinfo).
func stripURLUserinfo(raw string) string {
	scheme := strings.Index(raw, "://")
	if scheme < 0 {
		return raw
	}
	rest := raw[scheme+3:]
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	at := strings.LastIndex(rest[:end], "@")
	if at < 0 {
		return raw
	}
	return raw[:scheme+3] + rest[at+1:]
}

// sameIgnoringURLUserinfo reports whether two URLs are equal apart from
// their userinfo (scheme and host case-insensitively, like the API stores them).
func sameIgnoringURLUserinfo(a, b string) bool {
	if !strings.Contains(a, "://") || !strings.Contains(b, "://") {
		return false
	}
	return strings.EqualFold(stripURLUserinfo(a), stripURLUserinfo(b))
}

// keepPriorURL reports whether the configured URL should be kept for a
// remote URL: same apart from host case, or same apart from a masked userinfo.
func keepPriorURL(prior, remote string) bool {
	if strings.EqualFold(prior, remote) {
		return true
	}
	return isMaskedSecret(remote) && sameIgnoringURLUserinfo(prior, remote)
}
