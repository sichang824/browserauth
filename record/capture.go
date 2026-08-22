package record

import (
	"encoding/base64"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
)

// capture accumulates network events into HAR entries.
// All exported-to-package state is guarded by mu; the chromedp listener
// goroutine and the body-fetch workers share it.
type capture struct {
	mu     sync.Mutex
	active map[network.RequestID]*entryData // latest entry per request id
	order  []*entryData                     // first-seen order; redirect hops keep position

	// ExtraInfo events may arrive BEFORE their base event; buffer them by id.
	pendingReqExtra  map[network.RequestID]network.Headers
	pendingRespExtra map[network.RequestID]network.Headers

	wallBase time.Time          // recording start wall clock (fallback clock source)
	monoBase *cdp.MonotonicTime // first monotonic timestamp seen
}

// entryData is one logical request (or one redirect hop) under assembly.
type entryData struct {
	requestID network.RequestID
	method    string
	url       string
	resType   network.ResourceType

	reqHeaders  network.Headers
	wallStart   *cdp.TimeSinceEpoch
	monoStart   *cdp.MonotonicTime
	hasPostData bool
	postData    string // decoded from PostDataEntries (may be empty when Chrome dropped it)

	// Wire-fidelity upgrades from *ExtraInfo events (may arrive in any order).
	wireReqHeaders  network.Headers
	wireRespHeaders network.Headers

	resp     *network.Response
	respMono *cdp.MonotonicTime

	finishedMono *cdp.MonotonicTime
	failErr      string

	// Body — written only by workers, after fetch.
	bodyFetched   bool
	body          []byte
	bodyFullSize  int
	bodyTruncated bool
	bodyErr       string
}

func newCapture() *capture {
	return &capture{
		active:           make(map[network.RequestID]*entryData),
		pendingReqExtra:  make(map[network.RequestID]network.Headers),
		pendingRespExtra: make(map[network.RequestID]network.Headers),
		wallBase:         time.Now(),
	}
}

// observeClock anchors the monotonic→wall fallback clock on the first event
// carrying both timestamps.
func (c *capture) observeClock(wall *cdp.TimeSinceEpoch, mono *cdp.MonotonicTime) {
	if wall == nil || mono == nil {
		return
	}
	c.wallBase = wall.Time()
	c.monoBase = mono
}

// requestWillBeSent records a new request or, when RedirectResponse is set,
// finalizes the previous hop (same RequestID is reused across hops) and
// restarts the slot.
func (c *capture) requestWillBeSent(ev *network.EventRequestWillBeSent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.observeClock(ev.WallTime, ev.Timestamp)

	if prev := c.active[ev.RequestID]; prev != nil && ev.RedirectResponse != nil {
		prev.resp = ev.RedirectResponse
		prev.resType = ev.Type
		delete(c.active, ev.RequestID) // finalized hop; no body fetch for redirects
	}

	entry := &entryData{
		requestID: ev.RequestID,
		resType:   ev.Type,
		wallStart: ev.WallTime,
		monoStart: ev.Timestamp,
	}
	if req := ev.Request; req != nil {
		entry.method = req.Method
		entry.url = req.URL
		entry.reqHeaders = req.Headers
		entry.hasPostData = req.HasPostData
		entry.postData = postDataFromRequest(req)
	}
	if extra := c.pendingReqExtra[ev.RequestID]; extra != nil {
		entry.wireReqHeaders = extra
		delete(c.pendingReqExtra, ev.RequestID)
	}
	c.active[ev.RequestID] = entry
	c.order = append(c.order, entry)
}

// responseReceived attaches response metadata.
func (c *capture) responseReceived(ev *network.EventResponseReceived) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.active[ev.RequestID]
	if entry == nil {
		return
	}
	entry.resp = ev.Response
	entry.respMono = ev.Timestamp
	if ev.Type != "" {
		entry.resType = ev.Type
	}
	if extra := c.pendingRespExtra[ev.RequestID]; extra != nil {
		entry.wireRespHeaders = extra
		delete(c.pendingRespExtra, ev.RequestID)
	}
}

// requestExtraInfo stores the wire request headers (actual Cookie as sent).
// It may precede requestWillBeSent — buffer it in that case.
func (c *capture) requestExtraInfo(ev *network.EventRequestWillBeSentExtraInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.active[ev.RequestID]; entry != nil {
		entry.wireReqHeaders = ev.Headers
		return
	}
	c.pendingReqExtra[ev.RequestID] = ev.Headers
}

// responseExtraInfo stores the wire response headers (may precede the base event).
func (c *capture) responseExtraInfo(ev *network.EventResponseReceivedExtraInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.active[ev.RequestID]; entry != nil {
		entry.wireRespHeaders = ev.Headers
		return
	}
	c.pendingRespExtra[ev.RequestID] = ev.Headers
}

// loadingFailed marks the entry terminal with the failure reason.
func (c *capture) loadingFailed(ev *network.EventLoadingFailed) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.active[ev.RequestID]
	if entry == nil {
		return
	}
	entry.failErr = ev.ErrorText
	if ev.Canceled {
		entry.failErr += " (canceled)"
	}
	if entry.resType == "" {
		entry.resType = ev.Type
	}
	delete(c.active, ev.RequestID)
}

// loadingFinished marks timing and returns the entry so the caller can queue
// a body fetch. The entry stays active: CDP occasionally delivers a late
// ResponseReceived after Finished, and it should still attach.
func (c *capture) loadingFinished(ev *network.EventLoadingFinished) *entryData {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.active[ev.RequestID]
	if entry == nil {
		return nil
	}
	entry.finishedMono = ev.Timestamp
	return entry
}

// bodyEligible checks, under lock, whether a body fetch is worthwhile.
func (c *capture) bodyEligible(entry *entryData) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return shouldCaptureBody(entry.resType, mimeOf(entry))
}

// storeBody records the fetched body (truncated to maxBodyBytes).
func (c *capture) storeBody(entry *entryData, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry.bodyFetched = true
	entry.bodyFullSize = len(data)
	if len(data) > maxBodyBytes {
		data = data[:maxBodyBytes]
		entry.bodyTruncated = true
	}
	entry.body = data
}

// storeBodyErr records a failed body fetch (non-fatal).
func (c *capture) storeBodyErr(entry *entryData, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry.bodyFetched = false
	entry.bodyErr = err.Error()
}

// postDataFromRequest reassembles the request body from base64 chunks.
func postDataFromRequest(req *network.Request) string {
	if len(req.PostDataEntries) == 0 {
		return ""
	}
	var parts []byte
	for _, chunk := range req.PostDataEntries {
		dec, err := base64.StdEncoding.DecodeString(chunk.Bytes)
		if err != nil {
			continue
		}
		parts = append(parts, dec...)
	}
	return string(parts)
}
