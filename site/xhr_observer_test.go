package site

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
)

func TestXHRObserverCapturesOnlyXHRAndFetch(t *testing.T) {
	observer := newXHRObserver(context.Background())
	observer.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID: "document",
		Type:      network.ResourceTypeDocument,
		Request:   &network.Request{Method: "GET", URL: "https://example.test/"},
	})
	requestBody := `{"page":2}`
	observer.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID: "feed",
		Type:      network.ResourceTypeFetch,
		Request: &network.Request{
			Method:          "POST",
			URL:             "https://example.test/api/feed?page=2",
			HasPostData:     true,
			PostDataEntries: []*network.PostDataEntry{{Bytes: base64.StdEncoding.EncodeToString([]byte(requestBody))}},
		},
	})
	observer.responseReceived(&network.EventResponseReceived{
		RequestID: "feed",
		Type:      network.ResourceTypeFetch,
		Response:  &network.Response{Status: 200, MimeType: "application/json"},
	})
	observer.finishWithBody("feed", []byte(`{"items":[1,2]}`), "")

	status := observer.status()
	if status.Count != 1 || status.Completed != 1 || status.LatestSequence != 1 {
		t.Fatalf("unexpected status: %+v", status)
	}
	items := observer.list("/feed", 10)
	if len(items) != 1 || items[0].ID != "xhr-1" || items[0].ResponseBody != "" {
		t.Fatalf("unexpected list: %+v", items)
	}
	item, ok := observer.get("xhr-1")
	if !ok || item.RequestBody != requestBody || item.ResponseBody != `{"items":[1,2]}` {
		t.Fatalf("unexpected record: %+v", item)
	}
}

func TestXHRObserverMarksBrowserauthAndTruncatesBody(t *testing.T) {
	observer := newXHRObserver(context.Background())
	requestURL := "https://example.test/api/me?aid=6383"
	observer.markBrowserauth("get", requestURL)
	observer.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID: "me",
		Type:      network.ResourceTypeXHR,
		Request:   &network.Request{Method: "GET", URL: requestURL + "&a_bogus=page-added-signature"},
	})
	observer.finishWithBody("me", []byte(strings.Repeat("x", xhrMaxBodySize+1)), "")

	item, ok := observer.get("xhr-1")
	if !ok || item.Source != "browserauth" || !item.ResponseTruncated || len(item.ResponseBody) != xhrMaxBodySize {
		t.Fatalf("unexpected record: source=%q truncated=%t body=%d", item.Source, item.ResponseTruncated, len(item.ResponseBody))
	}
}

func TestXHRObserverWaitUsesSequenceBoundary(t *testing.T) {
	observer := newXHRObserver(context.Background())
	observer.requestWillBeSent(&network.EventRequestWillBeSent{
		RequestID: "old",
		Type:      network.ResourceTypeXHR,
		Request:   &network.Request{Method: "GET", URL: "https://example.test/api/feed/old"},
	})
	observer.finishWithBody("old", nil, "")

	go func() {
		time.Sleep(10 * time.Millisecond)
		observer.requestWillBeSent(&network.EventRequestWillBeSent{
			RequestID: "new",
			Type:      network.ResourceTypeFetch,
			Request:   &network.Request{Method: "GET", URL: "https://example.test/api/feed/new"},
		})
		observer.finishWithBody("new", []byte("ok"), "")
	}()
	record, err := observer.wait("/feed/", 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != "xhr-2" || record.ResponseBody != "ok" {
		t.Fatalf("unexpected record: %+v", record)
	}
}
