package httpx

import "testing"

func TestOriginAllowed_RejectsWildcardAndLookalikes(t *testing.T) {
	allowed := []string{"https://chat-online-zeta.vercel.app"}

	cases := []struct {
		origin string
		want   bool
	}{
		{"https://chat-online-zeta.vercel.app", true},
		{"https://CHAT-ONLINE-ZETA.vercel.app/", true},
		{"https://attacker.vercel.app", false},
		{"https://chat-online-zeta.vercel.app.evil.com", false},
		{"http://chat-online-zeta.vercel.app", false},
		{"null", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := OriginAllowed(tc.origin, "api.example.com", allowed); got != tc.want {
			t.Errorf("OriginAllowed(%q) = %v, want %v", tc.origin, got, tc.want)
		}
	}

	// A wildcard entry in configuration must never match anything.
	if OriginAllowed("https://attacker.vercel.app", "api.example.com", []string{"https://*.vercel.app"}) {
		t.Fatal("wildcard allowed origin must not match")
	}

	// Same-origin requests stay allowed.
	if !OriginAllowed("https://api.example.com", "api.example.com", nil) {
		t.Fatal("same-origin request must be allowed")
	}
}

func TestNormalizeOrigin_Validation(t *testing.T) {
	invalid := []string{"https://*.vercel.app", "*", "https://app.example.com/path", "ftp://x.com", "app.example.com", "https://user@app.example.com"}
	for _, raw := range invalid {
		if _, err := NormalizeOrigin(raw); err == nil {
			t.Errorf("NormalizeOrigin(%q) expected error", raw)
		}
	}
	if got, err := NormalizeOrigin("HTTPS://App.Example.com:8443/"); err != nil || got != "https://app.example.com:8443" {
		t.Fatalf("unexpected normalize result %q, %v", got, err)
	}
}

