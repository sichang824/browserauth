package cmd

import "testing"

func TestParseIsolatedProfileFlag(t *testing.T) {
	rest, isolated := parseIsolatedProfileFlag([]string{"--isolated-profile"})
	if !isolated || len(rest) != 0 {
		t.Fatalf("rest=%v isolated=%v", rest, isolated)
	}

	rest, isolated = parseIsolatedProfileFlag([]string{"--isolated-profile", "ignored"})
	if !isolated || len(rest) != 1 || rest[0] != "ignored" {
		t.Fatalf("rest=%v isolated=%v", rest, isolated)
	}

	rest, isolated = parseIsolatedProfileFlag(nil)
	if isolated || len(rest) != 0 {
		t.Fatalf("rest=%v isolated=%v", rest, isolated)
	}
}
