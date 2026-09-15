package site

import (
	"strings"
	"testing"
)

func TestRequestPanelIncludesRequiredControls(t *testing.T) {
	for _, required := range []string{
		"__browserauth-request-panel",
		"data-action=\"fullscreen\"",
		"data-action=\"collapse\"",
		"pointerdown",
		"setPointerCapture",
		"拖动面板",
		"请求体",
		"响应体",
		"sessionStorage",
	} {
		if !strings.Contains(requestPanelScript, required) {
			t.Fatalf("request panel script does not contain %q", required)
		}
	}
}

func TestRequestPanelDoesNotRenderAuthenticationHeaders(t *testing.T) {
	for _, forbidden := range []string{"Authorization", "Cookie", "extraHeaders"} {
		if strings.Contains(requestPanelScript, forbidden) {
			t.Fatalf("request panel script unexpectedly contains sensitive header field %q", forbidden)
		}
	}
}
