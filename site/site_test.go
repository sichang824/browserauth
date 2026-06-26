package site

import "testing"

func TestJsonPathNested(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"userId":   float64(410342),
			"nickName": "sichang824",
		},
	}
	v, ok := jsonPath(payload, "data.nickName")
	if !ok || v != "sichang824" {
		t.Fatalf("got %v ok=%v", v, ok)
	}
}

func TestForbiddenUsername(t *testing.T) {
	if !forbiddenUsername("anonymous", []string{"anonymous"}) {
		t.Fatal("expected forbidden")
	}
	if forbiddenUsername("alice", []string{"anonymous"}) {
		t.Fatal("expected allowed")
	}
}

func TestToInt64(t *testing.T) {
	if got := toInt64(float64(410342)); got != 410342 {
		t.Fatalf("got %d", got)
	}
}
