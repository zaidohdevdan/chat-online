package httpx

import (
	"errors"
	"net/url"
	"strings"
)

// ErrInvalidOrigin is returned when an ALLOWED_ORIGINS entry is not an exact scheme://host[:port] origin.
var ErrInvalidOrigin = errors.New("invalid origin")

// NormalizeOrigin validates that raw is an exact origin (scheme://host[:port], no wildcard,
// path, query or fragment) and returns it lowercased. Wildcards are rejected on purpose:
// credentialed CORS must never trust a whole hosting domain (e.g. *.vercel.app).
func NormalizeOrigin(raw string) (string, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(raw), "/")
	if trimmed == "" || strings.Contains(trimmed, "*") {
		return "", ErrInvalidOrigin
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrInvalidOrigin
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", ErrInvalidOrigin
	}
	return scheme + "://" + strings.ToLower(u.Host), nil
}

// OriginAllowed reports whether origin exactly matches one of the allowed origins,
// or is the same origin as the request host. No suffix or wildcard matching is performed.
func OriginAllowed(origin, requestHost string, allowedOrigins []string) bool {
	normalized, err := NormalizeOrigin(origin)
	if err != nil {
		return false
	}
	for _, allowed := range allowedOrigins {
		if a, aErr := NormalizeOrigin(allowed); aErr == nil && a == normalized {
			return true
		}
	}
	if u, err := url.Parse(normalized); err == nil && requestHost != "" {
		return strings.EqualFold(u.Host, requestHost)
	}
	return false
}

