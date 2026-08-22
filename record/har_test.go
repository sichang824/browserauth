package record

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
)

func mono(t time.Time) *cdp.MonotonicTime {
	m := cdp.MonotonicTime(t)
	return &m
}

func wall(t time.Time) *cdp.TimeSinceEpoch {
	w := cdp.TimeSinceEpoch(t)
	return &w
}

func TestHeadersToNVSortsAndSplitsDuplicates(t *testing.T) {
	nv := headersToNV(network.Headers{
		"Content-Type": "application/json",
		"Set-Cookie":   "a=1\nb=2",
		"X-Empty":      nil,
	})
	want := []HarNV{
		{Name: "Content-Type", Value: "application/json"},
		{Name: "Set-Cookie", Value: "a=1"},
		{Name: "Set-Cookie", Value: "b=2"},
	}
	if len(nv) != len(want) {
		t.Fatalf("got %d pairs, want %d: %+v", len(nv), len(want), nv)
	}
	for i := range want {
		if nv[i] != want[i] {
			t.Fatalf("pair %d = %+v, want %+v", i, nv[i], want[i])
		}
	}
}

func TestHeadersToNVDeterministic(t *testing.T) {
	h := network.Headers{"b": "2", "a": "1", "c": "3"}
	first := headersToNV(h)
	for i := 0; i < 10; i++ {
		if got := headersToNV(h); len(got) != len(first) || got[0] != first[0] || got[2] != first[2] {
			t.Fatalf("non-deterministic: %+v vs %+v", got, first)
		}
	}
}

func TestParseQueryString(t *testing.T) {
	cases := []struct {
		url  string
		want []HarNV
	}{
		{"https://x.test/p", nil},
		{"https://x.test/p?a=1&b=two%20words", []HarNV{{"a", "1"}, {"b", "two words"}}},
		{"https://x.test/p?a=1&a=2", []HarNV{{"a", "1"}, {"a", "2"}}},
		{"https://x.test/p?flag", []HarNV{{"flag", ""}}},
		{"https://x.test/p?a=1#frag=ignored", []HarNV{{"a", "1"}}},
		{"https://x.test/p?bad=%zz", []HarNV{{"bad", "%zz"}}},
	}
	for _, tc := range cases {
		got := parseQueryString(tc.url)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %+v, want %+v", tc.url, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: pair %d = %+v, want %+v", tc.url, i, got[i], tc.want[i])
			}
		}
	}
}

func TestParseCookieHeader(t *testing.T) {
	got := parseCookieHeader("a=1; b=x=y;  c=3")
	want := []HarCookie{{"a", "1"}, {"b", "x=y"}, {"c", "3"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("cookie %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got := parseCookieHeader(""); len(got) != 0 {
		t.Fatalf("empty header should yield no cookies, got %+v", got)
	}
}

func TestParseSetCookies(t *testing.T) {
	got := parseSetCookies(network.Headers{
		"Set-Cookie": "a=1; Path=/; HttpOnly\nb=2; Secure",
		"Other":      "nope",
	})
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0] != (HarCookie{"a", "1"}) || got[1] != (HarCookie{"b", "2"}) {
		t.Fatalf("unexpected cookies: %+v", got)
	}
}

func TestHttpVersionOf(t *testing.T) {
	cases := []struct {
		protocol, want string
	}{
		{"h2", "h2"},
		{"h3", "h3"},
		{"http/1.1", "HTTP/1.1"},
		{"", "HTTP/1.1"},
		{"http/1.0", "HTTP/1.0"},
	}
	for _, tc := range cases {
		if got := httpVersionOf(&network.Response{Protocol: tc.protocol}); got != tc.want {
			t.Fatalf("protocol %q: got %q, want %q", tc.protocol, got, tc.want)
		}
	}
	if got := httpVersionOf(nil); got != "HTTP/1.1" {
		t.Fatalf("nil response: got %q", got)
	}
}

func TestDecideBodyEncoding(t *testing.T) {
	text, enc := decideBodyEncoding("application/json", []byte(`{"a":1}`))
	if text != `{"a":1}` || enc != "" {
		t.Fatalf("json body: text=%q enc=%q", text, enc)
	}

	binary := []byte{0xff, 0xfe, 0x00}
	text, enc = decideBodyEncoding("application/json", binary)
	if enc != "base64" || text != base64.StdEncoding.EncodeToString(binary) {
		t.Fatalf("invalid utf8: text=%q enc=%q", text, enc)
	}

	text, enc = decideBodyEncoding("image/webp", []byte("riffdata"))
	if enc != "base64" {
		t.Fatalf("image mime should be base64, got enc=%q", enc)
	}
}

func TestShouldCaptureBody(t *testing.T) {
	cases := []struct {
		resType network.ResourceType
		mime    string
		want    bool
	}{
		{network.ResourceTypeXHR, "application/json", true},
		{network.ResourceTypeFetch, "text/html", true},
		{network.ResourceTypeDocument, "text/html", true},
		{network.ResourceTypeImage, "image/png", false},
		{network.ResourceTypeScript, "application/javascript", false},
		{network.ResourceTypeStylesheet, "text/css", false},
		{network.ResourceTypeFont, "font/woff2", false},
		{network.ResourceTypeMedia, "video/mp4", false},
		{network.ResourceTypeWebSocket, "", false},
		{network.ResourceTypeXHR, "image/webp", false}, // XHR fetching an image
		{network.ResourceTypeFetch, "application/octet-stream", false},
		{network.ResourceTypeOther, "application/json", true},
	}
	for _, tc := range cases {
		if got := shouldCaptureBody(tc.resType, tc.mime); got != tc.want {
			t.Fatalf("shouldCaptureBody(%s, %s) = %v, want %v", tc.resType, tc.mime, got, tc.want)
		}
	}
}

func TestTimingsOfUsesResourceTimingAndMonotonicDelta(t *testing.T) {
	respTime := time.Now()
	e := &entryData{
		resp: &network.Response{
			Timing: &network.ResourceTiming{
				SendStart:          1,
				SendEnd:            3,
				ReceiveHeadersEnd:  50,
				ProxyStart:         -1,
				ProxyEnd:           -1,
			},
		},
		respMono:     mono(respTime),
		finishedMono: mono(respTime.Add(120 * time.Millisecond)),
	}
	got := timingsOf(e)
	if got.Send != 2 || got.Wait != 47 {
		t.Fatalf("send/wait = %v/%v, want 2/47", got.Send, got.Wait)
	}
	if got.Receive < 119 || got.Receive > 121 {
		t.Fatalf("receive = %v, want ~120", got.Receive)
	}
}

func TestTimingsClampsNegativeFields(t *testing.T) {
	e := &entryData{
		resp: &network.Response{Timing: &network.ResourceTiming{
			SendStart:         -1,
			SendEnd:           -1,
			ReceiveHeadersEnd: -1,
		}},
	}
	got := timingsOf(e)
	if got.Send != 0 || got.Wait != 0 || got.Receive != 0 {
		t.Fatalf("expected zeroed timings, got %+v", got)
	}
}

func TestStartedDateTimeFallbackUsesMonotonicDelta(t *testing.T) {
	cap := newCapture()
	baseWall := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	baseMono := time.Now()
	cap.observeClock(wall(baseWall), mono(baseMono))

	e := &entryData{monoStart: mono(baseMono.Add(500 * time.Millisecond))}
	got := startedDateTimeOf(e, cap)
	if got.Sub(baseWall) < 400*time.Millisecond || got.Sub(baseWall) > 600*time.Millisecond {
		t.Fatalf("fallback time %v not ~500ms after %v", got, baseWall)
	}

	e2 := &entryData{wallStart: wall(baseWall.Add(2 * time.Second))}
	if got := startedDateTimeOf(e2, cap); !got.Equal(baseWall.Add(2 * time.Second)) {
		t.Fatalf("wallStart preferred: got %v", got)
	}
}

func TestBuildHARShapeAndRedirectChain(t *testing.T) {
	cap := newCapture()
	t0 := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

	// Hop 1: GET that gets 302-redirected (same RequestID reused).
	cap.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID: "1.1",
		Request: &network.Request{
			URL:     "https://a.test/start?next=%2Fhome",
			Method:  "GET",
			Headers: network.Headers{"Cookie": "web_session=abc; a1=xyz"},
		},
		Timestamp: mono(t0),
		WallTime:  wall(t0),
		Type:      network.ResourceTypeDocument,
	})
	cap.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID:        "1.1",
		RedirectResponse: &network.Response{Status: 302, StatusText: "Found", Headers: network.Headers{"Location": "/home"}, Protocol: "h2"},
		Request:          &network.Request{URL: "https://a.test/home", Method: "GET", Headers: network.Headers{}},
		Timestamp:        mono(t0.Add(30 * time.Millisecond)),
		WallTime:         wall(t0.Add(30 * time.Millisecond)),
		Type:             network.ResourceTypeDocument,
	})

	// Hop 2 response + body.
	cap.responseReceived(&network.EventResponseReceived{
		RequestID: "1.1",
		Timestamp: mono(t0.Add(60 * time.Millisecond)),
		Type:      network.ResourceTypeDocument,
		Response: &network.Response{
			Status:     200,
			StatusText: "OK",
			MimeType:   "application/json",
			Protocol:   "h2",
			Headers:    network.Headers{"Content-Type": "application/json", "Set-Cookie": "sid=42; Path=/"},
		},
	})
	entry := cap.loadingFinished(&network.EventLoadingFinished{
		RequestID:         "1.1",
		Timestamp:         mono(t0.Add(90 * time.Millisecond)),
		EncodedDataLength: 12,
	})
	if entry == nil {
		t.Fatal("loadingFinished should return the active entry")
	}
	cap.storeBody(entry, []byte(`{"ok":true}`))

	har := buildHAR(cap)
	if har.Log.Version != "1.2" || har.Log.Creator.Name == "" {
		t.Fatalf("bad log header: %+v", har.Log)
	}
	if len(har.Log.Entries) != 2 {
		t.Fatalf("redirect chain should yield 2 entries, got %d", len(har.Log.Entries))
	}

	hop1, hop2 := har.Log.Entries[0], har.Log.Entries[1]
	if hop1.Response.Status != 302 || hop1.Response.RedirectURL != "/home" {
		t.Fatalf("hop1 = %+v", hop1.Response)
	}
	if hop1.Request.URL != "https://a.test/start?next=%2Fhome" {
		t.Fatalf("hop1 url = %q", hop1.Request.URL)
	}
	if len(hop1.Request.Cookies) != 2 || hop1.Request.Cookies[0].Name != "web_session" {
		t.Fatalf("hop1 cookies = %+v", hop1.Request.Cookies)
	}
	if hop2.Response.Status != 200 {
		t.Fatalf("hop2 status = %d", hop2.Response.Status)
	}
	if hop2.Response.Content.Text != `{"ok":true}` || hop2.Response.Content.Encoding != "" {
		t.Fatalf("hop2 content = %+v", hop2.Response.Content)
	}
	if hop2.Response.Content.Size != 11 || hop2.Response.BodySize != 11 {
		t.Fatalf("hop2 sizes = %d/%d", hop2.Response.Content.Size, hop2.Response.BodySize)
	}
	if len(hop2.Response.Cookies) != 1 || hop2.Response.Cookies[0] != (HarCookie{"sid", "42"}) {
		t.Fatalf("hop2 response cookies = %+v", hop2.Response.Cookies)
	}
	if hop1.StartedDateTime != "2026-08-18T12:00:00.000Z" {
		t.Fatalf("hop1 startedDateTime = %q", hop1.StartedDateTime)
	}
	if hop1.Request.HTTPVersion != "h2" {
		t.Fatalf("hop1 httpVersion = %q", hop1.Request.HTTPVersion)
	}
	if hop1.ResourceType != "Document" {
		t.Fatalf("hop1 _resourceType = %q", hop1.ResourceType)
	}

	data, err := json.Marshal(har)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"_resourceType":"Document"`) {
		t.Fatalf("extension field missing from JSON: %s", data)
	}
}

func TestBuildHARPostDataAndTruncation(t *testing.T) {
	cap := newCapture()
	t0 := time.Now()
	body := `{"action":"getTransStatus"}`
	chunk := base64.StdEncoding.EncodeToString([]byte(body))

	cap.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID: "2.1",
		Request: &network.Request{
			URL:             "https://a.test/api",
			Method:          "POST",
			Headers:         network.Headers{"Content-Type": "application/json"},
			HasPostData:     true,
			PostDataEntries: []*network.PostDataEntry{{Bytes: chunk}},
		},
		Timestamp: mono(t0),
		WallTime:  wall(t0),
		Type:      network.ResourceTypeXHR,
	})
	entry := cap.loadingFinished(&network.EventLoadingFinished{RequestID: "2.1", Timestamp: mono(t0)})
	cap.storeBody(entry, make([]byte, maxBodyBytes+100)) // force truncation

	har := buildHAR(cap)
	e := har.Log.Entries[0]
	if e.Request.PostData == nil || e.Request.PostData.Text != body {
		t.Fatalf("postData = %+v", e.Request.PostData)
	}
	if e.Request.PostData.MimeType != "application/json" {
		t.Fatalf("postData mimeType = %q", e.Request.PostData.MimeType)
	}
	if e.Request.BodySize != len(body) {
		t.Fatalf("request bodySize = %d", e.Request.BodySize)
	}
	if !e.BodyTruncated {
		t.Fatal("expected _bodyTruncated flag")
	}
	if e.Response.Content.Size != int64(maxBodyBytes+100) {
		t.Fatalf("content.size should be full decoded size, got %d", e.Response.Content.Size)
	}
}

func TestBuildHARPostDataLost(t *testing.T) {
	cap := newCapture()
	t0 := time.Now()
	cap.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID:     "3.1",
		Request:       &network.Request{URL: "https://a.test/big", Method: "POST", HasPostData: true},
		Timestamp:     mono(t0),
		WallTime:      wall(t0),
		Type:          network.ResourceTypeXHR,
	})
	har := buildHAR(cap)
	e := har.Log.Entries[0]
	if !e.PostDataLost {
		t.Fatal("expected _postDataLost when HasPostData but no entries")
	}
	if e.Request.BodySize != -1 {
		t.Fatalf("bodySize should stay -1, got %d", e.Request.BodySize)
	}
}
