package record

import (
	"errors"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
)

func TestLoadingFailedMarksError(t *testing.T) {
	cap := newCapture()
	t0 := time.Now()
	cap.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID: "f1",
		Request:   &network.Request{URL: "https://a.test/x", Method: "GET"},
		Timestamp: mono(t0),
		WallTime:  wall(t0),
		Type:      network.ResourceTypeFetch,
	})
	cap.loadingFailed(&network.EventLoadingFailed{
		RequestID: "f1",
		ErrorText: "net::ERR_ABORTED",
		Canceled:  true,
	})
	har := buildHAR(cap)
	if len(har.Log.Entries) != 1 {
		t.Fatalf("entries = %d", len(har.Log.Entries))
	}
	e := har.Log.Entries[0]
	if e.Error == "" || e.Response.Status != 0 {
		t.Fatalf("entry = %+v", e)
	}
	// Failed entry is no longer active: a late response must not attach.
	cap.responseReceived(&network.EventResponseReceived{
		RequestID: "f1",
		Response:  &network.Response{Status: 200},
	})
	har = buildHAR(cap)
	if har.Log.Entries[0].Response.Status != 0 {
		t.Fatal("response attached to a failed entry")
	}
}

func TestOutOfOrderFinishedBeforeResponse(t *testing.T) {
	cap := newCapture()
	t0 := time.Now()
	cap.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID: "o1",
		Request:   &network.Request{URL: "https://a.test/y", Method: "GET"},
		Timestamp: mono(t0),
		WallTime:  wall(t0),
		Type:      network.ResourceTypeXHR,
	})
	entry := cap.loadingFinished(&network.EventLoadingFinished{RequestID: "o1", Timestamp: mono(t0)})
	if entry == nil {
		t.Fatal("finished should return the entry even without a response yet")
	}
	cap.responseReceived(&network.EventResponseReceived{
		RequestID: "o1",
		Response:  &network.Response{Status: 204},
		Timestamp: mono(t0),
	})
	har := buildHAR(cap)
	if har.Log.Entries[0].Response.Status != 204 {
		t.Fatalf("late response should still attach, got %+v", har.Log.Entries[0].Response)
	}
}

func TestExtraInfoMergesRegardlessOfOrder(t *testing.T) {
	cap := newCapture()
	t0 := time.Now()

	// ExtraInfo arriving BEFORE the base event is buffered and merged on arrival.
	cap.requestExtraInfo(&network.EventRequestWillBeSentExtraInfo{
		RequestID: "e-early",
		Headers:   network.Headers{"Cookie": "early=1"},
	})
	cap.responseExtraInfo(&network.EventResponseReceivedExtraInfo{
		RequestID: "e-early",
		Headers:   network.Headers{"X-Early": "yes"},
	})
	cap.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID: "e-early",
		Request:   &network.Request{URL: "https://a.test/early", Method: "GET", Headers: network.Headers{"Accept": "*/*"}},
		Timestamp: mono(t0),
		WallTime:  wall(t0),
		Type:      network.ResourceTypeFetch,
	})
	cap.responseReceived(&network.EventResponseReceived{
		RequestID: "e-early",
		Response:  &network.Response{Status: 200, Headers: network.Headers{}},
	})
	early := buildHAR(cap).Log.Entries[0]
	var earlyCookie string
	for _, nv := range early.Request.Headers {
		if nv.Name == "Cookie" {
			earlyCookie = nv.Value
		}
	}
	if earlyCookie != "early=1" {
		t.Fatalf("early ExtraInfo not merged, headers = %+v", early.Request.Headers)
	}
	var earlyRespHeader string
	for _, nv := range early.Response.Headers {
		if nv.Name == "X-Early" {
			earlyRespHeader = nv.Value
		}
	}
	if earlyRespHeader != "yes" {
		t.Fatalf("early response ExtraInfo not merged, headers = %+v", early.Response.Headers)
	}

	cap.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID: "e1",
		Request:   &network.Request{URL: "https://a.test/z", Method: "GET", Headers: network.Headers{"Accept": "*/*"}},
		Timestamp: mono(t0),
		WallTime:  wall(t0),
		Type:      network.ResourceTypeFetch,
	})
	cap.requestExtraInfo(&network.EventRequestWillBeSentExtraInfo{
		RequestID: "e1",
		Headers:   network.Headers{"Cookie": "web_session=s3cret"},
	})
	cap.responseReceived(&network.EventResponseReceived{
		RequestID: "e1",
		Timestamp: mono(t0),
		Response:  &network.Response{Status: 200, MimeType: "application/json", Headers: network.Headers{}},
	})
	cap.responseExtraInfo(&network.EventResponseReceivedExtraInfo{
		RequestID:  "e1",
		Headers:    network.Headers{"Content-Type": "application/json", "Set-Cookie": "a=1\nb=2"},
		StatusCode: 200,
	})

	har := buildHAR(cap)
	e := har.Log.Entries[1] // [0] is the early-ExtraInfo request above
	// Wire request headers take priority over Request.Headers.
	var sawCookie bool
	for _, nv := range e.Request.Headers {
		if nv.Name == "Cookie" && nv.Value == "web_session=s3cret" {
			sawCookie = true
		}
	}
	if !sawCookie {
		t.Fatalf("wire request headers not preferred: %+v", e.Request.Headers)
	}
	if len(e.Request.Cookies) != 1 || e.Request.Cookies[0].Value != "s3cret" {
		t.Fatalf("request cookies = %+v", e.Request.Cookies)
	}
	if len(e.Response.Cookies) != 2 {
		t.Fatalf("response cookies = %+v", e.Response.Cookies)
	}
}

func TestRedirectKeepsOrderSlot(t *testing.T) {
	cap := newCapture()
	t0 := time.Now()

	cap.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID: "r-first",
		Request:   &network.Request{URL: "https://a.test/1", Method: "GET"},
		Timestamp: mono(t0),
		WallTime:  wall(t0),
	})
	cap.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID: "r-second",
		Request:   &network.Request{URL: "https://a.test/2", Method: "GET"},
		Timestamp: mono(t0.Add(time.Millisecond)),
		WallTime:  wall(t0.Add(time.Millisecond)),
	})
	// Redirect on the SECOND id: the hop must appear before any later request,
	// at the second id's original position.
	cap.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID:        "r-second",
		RedirectResponse: &network.Response{Status: 301},
		Request:          &network.Request{URL: "https://a.test/3", Method: "GET"},
		Timestamp:        mono(t0.Add(2 * time.Millisecond)),
		WallTime:         wall(t0.Add(2 * time.Millisecond)),
	})

	har := buildHAR(cap)
	urls := make([]string, 0, len(har.Log.Entries))
	for _, e := range har.Log.Entries {
		urls = append(urls, e.Request.URL)
	}
	want := []string{"https://a.test/1", "https://a.test/2", "https://a.test/3"}
	if len(urls) != 3 {
		t.Fatalf("entries = %+v", urls)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Fatalf("order = %+v, want %+v", urls, want)
		}
	}
	if har.Log.Entries[1].Response.Status != 301 {
		t.Fatalf("redirect hop status = %d", har.Log.Entries[1].Response.Status)
	}
}

func TestPostDataFromEntriesConcatenatesChunks(t *testing.T) {
	req := &network.Request{
		HasPostData: true,
		PostDataEntries: []*network.PostDataEntry{
			{Bytes: "eyJh"}, // {"a
			{Bytes: "Ijox"}, // ":1
			{Bytes: "fQ=="}, // }
		},
	}
	if got := postDataFromRequest(req); got != `{"a":1}` {
		t.Fatalf("postData = %q", got)
	}
	if got := postDataFromRequest(&network.Request{HasPostData: true}); got != "" {
		t.Fatalf("no entries should yield empty body, got %q", got)
	}
}

func TestStoreBodyTruncatesAndKeepsFullSize(t *testing.T) {
	cap := newCapture()
	e := &entryData{}
	cap.storeBody(e, make([]byte, maxBodyBytes+7))
	if !e.bodyTruncated || len(e.body) != maxBodyBytes || e.bodyFullSize != maxBodyBytes+7 {
		t.Fatalf("truncation wrong: truncated=%v len=%d full=%d", e.bodyTruncated, len(e.body), e.bodyFullSize)
	}
}

func TestStoreBodyErrRecordsMessage(t *testing.T) {
	cap := newCapture()
	e := &entryData{}
	cap.storeBodyErr(e, errors.New("no data found for resource"))
	if e.bodyErr == "" || e.bodyFetched {
		t.Fatalf("bodyErr = %q fetched = %v", e.bodyErr, e.bodyFetched)
	}
}

func TestBodyEligible(t *testing.T) {
	cap := newCapture()
	xhr := &entryData{resType: network.ResourceTypeXHR, resp: &network.Response{MimeType: "application/json"}}
	img := &entryData{resType: network.ResourceTypeImage, resp: &network.Response{MimeType: "image/png"}}
	if !cap.bodyEligible(xhr) {
		t.Fatal("XHR json body should be eligible")
	}
	if cap.bodyEligible(img) {
		t.Fatal("image body should be skipped")
	}
}
