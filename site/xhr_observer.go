package site

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const (
	xhrMaxEntries  = 200
	xhrMaxBodySize = 1024 * 1024
)

// XHRRecord is one page-native XHR or Fetch request observed in a retained
// browser session. Authentication headers are intentionally never collected.
type XHRRecord struct {
	ID                string    `json:"id"`
	Sequence          int64     `json:"sequence"`
	ResourceType      string    `json:"resource_type"`
	Method            string    `json:"method"`
	URL               string    `json:"url"`
	Status            int64     `json:"status,omitempty"`
	State             string    `json:"state"`
	StartedAt         time.Time `json:"started_at"`
	DurationMS        int64     `json:"duration_ms,omitempty"`
	RequestBody       string    `json:"request_body,omitempty"`
	ResponseBody      string    `json:"response_body,omitempty"`
	ResponseSize      int64     `json:"response_size,omitempty"`
	ResponseTruncated bool      `json:"response_truncated,omitempty"`
	MIMEType          string    `json:"mime_type,omitempty"`
	Error             string    `json:"error,omitempty"`
	Source            string    `json:"source"`

	requestID network.RequestID
}

// XHRStatus summarizes the observer attached to a retained browser session.
type XHRStatus struct {
	Enabled        bool  `json:"enabled"`
	Count          int   `json:"count"`
	Pending        int   `json:"pending"`
	Completed      int   `json:"completed"`
	Failed         int   `json:"failed"`
	LatestSequence int64 `json:"latest_sequence"`
}

type xhrObserver struct {
	ctx     context.Context
	mu      sync.Mutex
	enabled bool
	next    int64
	order   []*XHRRecord
	byID    map[string]*XHRRecord
	active  map[network.RequestID]*XHRRecord
	jobs    chan network.RequestID
	changed chan struct{}
	marked  map[string]int
}

func newXHRObserver(ctx context.Context) *xhrObserver {
	return &xhrObserver{
		ctx:     ctx,
		byID:    make(map[string]*XHRRecord),
		active:  make(map[network.RequestID]*XHRRecord),
		jobs:    make(chan network.RequestID, 32),
		changed: make(chan struct{}),
		marked:  make(map[string]int),
	}
}

func (o *xhrObserver) enable() error {
	o.mu.Lock()
	if o.enabled {
		o.mu.Unlock()
		return nil
	}
	o.enabled = true
	o.mu.Unlock()

	if err := chromedp.Run(o.ctx, network.Enable()); err != nil {
		o.mu.Lock()
		o.enabled = false
		o.mu.Unlock()
		return err
	}
	chromedp.ListenTarget(o.ctx, o.handleEvent)
	go o.bodyWorker()
	return nil
}

func (o *xhrObserver) handleEvent(event any) {
	switch ev := event.(type) {
	case *network.EventRequestWillBeSent:
		o.requestWillBeSent(ev)
	case *network.EventResponseReceived:
		o.responseReceived(ev)
	case *network.EventLoadingFinished:
		o.loadingFinished(ev)
	case *network.EventLoadingFailed:
		o.loadingFailed(ev)
	}
}

func observedResourceType(kind network.ResourceType) bool {
	return kind == network.ResourceTypeXHR || kind == network.ResourceTypeFetch
}

func observerKey(method, rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Host != "" {
		// Page request hooks may append or replace signing query parameters
		// between fetch() and Network.requestWillBeSent. Match the stable
		// origin/path portion so those browserauth calls remain identifiable.
		rawURL = parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath()
	}
	return strings.ToUpper(method) + " " + rawURL
}

func (o *xhrObserver) markBrowserauth(method, rawURL string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.marked[observerKey(method, rawURL)]++
}

func (o *xhrObserver) requestWillBeSent(ev *network.EventRequestWillBeSent) {
	if ev.Request == nil || !observedResourceType(ev.Type) {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.next++
	source := "page"
	key := observerKey(ev.Request.Method, ev.Request.URL)
	if o.marked[key] > 0 {
		source = "browserauth"
		o.marked[key]--
		if o.marked[key] == 0 {
			delete(o.marked, key)
		}
	}
	record := &XHRRecord{
		ID:           fmt.Sprintf("xhr-%d", o.next),
		Sequence:     o.next,
		ResourceType: ev.Type.String(),
		Method:       ev.Request.Method,
		URL:          ev.Request.URL,
		State:        "pending",
		StartedAt:    time.Now(),
		RequestBody:  observerPostData(ev.Request),
		Source:       source,
		requestID:    ev.RequestID,
	}
	o.active[ev.RequestID] = record
	o.byID[record.ID] = record
	o.order = append(o.order, record)
	o.trimLocked()
	o.notifyLocked()
}

func (o *xhrObserver) responseReceived(ev *network.EventResponseReceived) {
	o.mu.Lock()
	defer o.mu.Unlock()
	record := o.active[ev.RequestID]
	if record == nil || ev.Response == nil {
		return
	}
	record.Status = ev.Response.Status
	record.MIMEType = ev.Response.MimeType
	if ev.Type != "" {
		record.ResourceType = ev.Type.String()
	}
	o.notifyLocked()
}

func (o *xhrObserver) loadingFinished(ev *network.EventLoadingFinished) {
	o.mu.Lock()
	record := o.active[ev.RequestID]
	if record == nil {
		o.mu.Unlock()
		return
	}
	record.ResponseSize = int64(ev.EncodedDataLength)
	o.mu.Unlock()
	select {
	case o.jobs <- ev.RequestID:
	default:
		o.finishWithBody(ev.RequestID, nil, "response body queue is full")
	}
}

func (o *xhrObserver) loadingFailed(ev *network.EventLoadingFailed) {
	o.mu.Lock()
	defer o.mu.Unlock()
	record := o.active[ev.RequestID]
	if record == nil {
		return
	}
	record.State = "failed"
	record.Error = ev.ErrorText
	if ev.Canceled {
		record.Error += " (canceled)"
	}
	record.DurationMS = time.Since(record.StartedAt).Milliseconds()
	delete(o.active, ev.RequestID)
	o.trimLocked()
	o.notifyLocked()
}

func (o *xhrObserver) bodyWorker() {
	for {
		select {
		case <-o.ctx.Done():
			return
		case requestID := <-o.jobs:
			var body []byte
			err := chromedp.Run(o.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				var fetchErr error
				body, fetchErr = network.GetResponseBody(requestID).Do(ctx)
				return fetchErr
			}))
			if err != nil {
				o.finishWithBody(requestID, nil, err.Error())
			} else {
				o.finishWithBody(requestID, body, "")
			}
		}
	}
}

func (o *xhrObserver) finishWithBody(requestID network.RequestID, body []byte, bodyErr string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	record := o.active[requestID]
	if record == nil {
		return
	}
	if len(body) > xhrMaxBodySize {
		record.ResponseBody = string(body[:xhrMaxBodySize])
		record.ResponseTruncated = true
	} else {
		record.ResponseBody = string(body)
	}
	if record.ResponseSize == 0 {
		record.ResponseSize = int64(len(body))
	}
	if bodyErr != "" {
		record.Error = bodyErr
	}
	record.State = "completed"
	record.DurationMS = time.Since(record.StartedAt).Milliseconds()
	delete(o.active, requestID)
	o.trimLocked()
	o.notifyLocked()
}

func (o *xhrObserver) trimLocked() {
	for len(o.order) > xhrMaxEntries {
		index := -1
		for i, record := range o.order {
			if record.State != "pending" {
				index = i
				break
			}
		}
		if index < 0 {
			return
		}
		delete(o.byID, o.order[index].ID)
		o.order = append(o.order[:index], o.order[index+1:]...)
	}
}

func (o *xhrObserver) notifyLocked() {
	close(o.changed)
	o.changed = make(chan struct{})
}

func (o *xhrObserver) status() XHRStatus {
	o.mu.Lock()
	defer o.mu.Unlock()
	status := XHRStatus{Enabled: o.enabled, Count: len(o.order), LatestSequence: o.next}
	for _, record := range o.order {
		switch record.State {
		case "pending":
			status.Pending++
		case "failed":
			status.Failed++
		default:
			status.Completed++
		}
	}
	return status
}

func (o *xhrObserver) list(match string, limit int) []XHRRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	if limit <= 0 || limit > xhrMaxEntries {
		limit = 50
	}
	result := make([]XHRRecord, 0, limit)
	for i := len(o.order) - 1; i >= 0 && len(result) < limit; i-- {
		record := o.order[i]
		if !matchesXHR(record, match) {
			continue
		}
		item := *record
		item.RequestBody = ""
		item.ResponseBody = ""
		item.requestID = ""
		result = append(result, item)
	}
	return result
}

func (o *xhrObserver) get(id string) (XHRRecord, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	record := o.byID[id]
	if record == nil {
		return XHRRecord{}, false
	}
	item := *record
	item.requestID = ""
	return item, true
}

func (o *xhrObserver) clear() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	removed := len(o.order)
	o.order = nil
	o.byID = make(map[string]*XHRRecord)
	for id, record := range o.active {
		o.byID[record.ID] = record
		o.order = append(o.order, record)
		_ = id
	}
	o.notifyLocked()
	return removed - len(o.order)
}

func (o *xhrObserver) wait(match string, after int64, timeout time.Duration) (XHRRecord, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		o.mu.Lock()
		for i := len(o.order) - 1; i >= 0; i-- {
			record := o.order[i]
			if record.Sequence > after && record.State != "pending" && matchesXHR(record, match) {
				item := *record
				item.requestID = ""
				o.mu.Unlock()
				return item, nil
			}
		}
		changed := o.changed
		o.mu.Unlock()
		select {
		case <-o.ctx.Done():
			return XHRRecord{}, o.ctx.Err()
		case <-timer.C:
			return XHRRecord{}, fmt.Errorf("timed out waiting for XHR matching %q", match)
		case <-changed:
		}
	}
}

func matchesXHR(record *XHRRecord, match string) bool {
	match = strings.ToLower(strings.TrimSpace(match))
	return match == "" || strings.Contains(strings.ToLower(record.URL), match)
}

func observerPostData(req *network.Request) string {
	var result []byte
	for _, item := range req.PostDataEntries {
		decoded, err := base64.StdEncoding.DecodeString(item.Bytes)
		if err == nil {
			result = append(result, decoded...)
		}
	}
	if len(result) > xhrMaxBodySize {
		result = result[:xhrMaxBodySize]
	}
	return string(result)
}
