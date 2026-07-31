package main

import "testing"

// The only property that matters: DEV_LOGIN must never take effect against
// anything but exactly the two forms `docker compose` + a local run actually
// produce - so copying a local .env.local into a real deployment by mistake
// leaves DEV_LOGIN silently inert rather than a real exposure.
func TestIsLocalhost(t *testing.T) {
	cases := map[string]bool{
		"http://localhost:8080":           true,
		"http://localhost":                true,
		"http://127.0.0.1:8080":           true,
		"https://coc-progress.vercel.app": false,
		"https://localhost":               false,
		"http://evil.com?localhost":       false,
		"":                                false,
	}
	for baseURL, want := range cases {
		if got := isLocalhost(baseURL); got != want {
			t.Errorf("isLocalhost(%q) = %v, want %v", baseURL, got, want)
		}
	}
}
