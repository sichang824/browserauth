package httpclient

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"skills-browserauth/site"
)

func TestDoWithBearerFromCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok123" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	auth := site.AuthConfig{
		Token: &site.TokenConfig{Cookie: "app_platform_token"},
	}
	client := NewDirect(srv.URL, "app_platform_token=tok123", nil)
	client.AuthHeaders = auth.AuthHeaders

	body, status, err := client.Do(http.MethodGet, "/api", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status %d body %s", status, body)
	}
}
