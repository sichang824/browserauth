package record

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chromedp/cdproto/network"
)

// HAR 1.2 structs (minimal subset, field names per spec).
// https://w3c.github.io/web-performance/specs/HAR/Overview.html

type HarLog struct {
	Log HarRoot `json:"log"`
}

type HarRoot struct {
	Version string     `json:"version"`
	Creator HarCreator `json:"creator"`
	Entries []HarEntry `json:"entries"`
}

type HarCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type HarEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         HarRequest  `json:"request"`
	Response        HarResponse `json:"response"`
	Timings         HarTimings  `json:"timings"`

	// Extension fields (HAR allows arbitrary "_" keys); aid downstream analysis.
	ResourceType   string `json:"_resourceType,omitempty"`
	FromCache      bool   `json:"_fromCache,omitempty"`
	Error          string `json:"_error,omitempty"`
	BodyTruncated  bool   `json:"_bodyTruncated,omitempty"`
	BodyError      string `json:"_bodyError,omitempty"`
	PostDataLost   bool   `json:"_postDataLost,omitempty"`
}

type HarRequest struct {
	Method      string        `json:"method"`
	URL         string        `json:"url"`
	HTTPVersion string        `json:"httpVersion"`
	Cookies     []HarCookie   `json:"cookies"`
	Headers     []HarNV       `json:"headers"`
	QueryString []HarNV       `json:"queryString"`
	PostData    *HarPostData  `json:"postData,omitempty"`
	HeadersSize int           `json:"headersSize"`
	BodySize    int           `json:"bodySize"`
}

type HarResponse struct {
	Status      int        `json:"status"`
	StatusText  string     `json:"statusText"`
	HTTPVersion string     `json:"httpVersion"`
	Cookies     []HarCookie `json:"cookies"`
	Headers     []HarNV    `json:"headers"`
	Content     HarContent `json:"content"`
	RedirectURL string     `json:"redirectURL"`
	HeadersSize int        `json:"headersSize"`
	BodySize    int        `json:"bodySize"`
}

type HarPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

type HarContent struct {
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

type HarCookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HarNV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HarTimings struct {
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
}

// buildHAR renders the capture state into a HAR document.
func buildHAR(cap *capture) HarLog {
	cap.mu.Lock()
	defer cap.mu.Unlock()

	entries := make([]HarEntry, 0, len(cap.order))
	for _, e := range cap.order {
		entries = append(entries, entryToHAR(e, cap))
	}
	return HarLog{Log: HarRoot{
		Version: "1.2",
		Creator: HarCreator{Name: "browserauth-record", Version: "1.0"},
		Entries: entries,
	}}
}

func entryToHAR(e *entryData, cap *capture) HarEntry {
	har := HarEntry{
		StartedDateTime: formatHARTime(startedDateTimeOf(e, cap)),
		Request: HarRequest{
			Method:      e.method,
			URL:         e.url,
			HTTPVersion: "HTTP/1.1", // request-side unknown; refined below when response exists
			Cookies:     []HarCookie{},
			Headers:     []HarNV{},
			QueryString: parseQueryString(e.url),
			HeadersSize: -1,
			BodySize:    -1,
		},
		Response: HarResponse{
			Cookies:     []HarCookie{},
			Headers:     []HarNV{},
			Content:     HarContent{Size: -1},
			HeadersSize: -1,
			BodySize:    -1,
		},
		ResourceType: string(e.resType),
	}

	reqHeaders := e.reqHeaders
	if len(e.wireReqHeaders) > 0 {
		reqHeaders = e.wireReqHeaders // ExtraInfo: headers as actually sent
	}
	har.Request.Headers = headersToNV(reqHeaders)
	for _, nv := range har.Request.Headers {
		if strings.EqualFold(nv.Name, "cookie") {
			har.Request.Cookies = append(har.Request.Cookies, parseCookieHeader(nv.Value)...)
		}
	}
	if e.postData != "" {
		har.Request.PostData = &HarPostData{
			MimeType: headerValue(reqHeaders, "Content-Type"),
			Text:     e.postData,
		}
		har.Request.BodySize = len(e.postData)
	} else if !e.hasPostData {
		har.Request.BodySize = 0
	} else {
		har.PostDataLost = true // HasPostData but Chrome dropped the body chunks
	}

	if e.resp != nil {
		respHeaders := e.resp.Headers
		if len(e.wireRespHeaders) > 0 {
			respHeaders = e.wireRespHeaders
		}
		har.Response = HarResponse{
			Status:      int(e.resp.Status),
			StatusText:  e.resp.StatusText,
			HTTPVersion: httpVersionOf(e.resp),
			Cookies:     parseSetCookies(respHeaders),
			Headers:     headersToNV(respHeaders),
			Content: HarContent{
				Size:     -1,
				MimeType: e.resp.MimeType,
			},
			RedirectURL: headerValue(respHeaders, "Location"),
			HeadersSize: -1,
			BodySize:    -1,
		}
		har.Request.HTTPVersion = httpVersionOf(e.resp)
		if e.resp.FromDiskCache || e.resp.FromPrefetchCache {
			har.FromCache = true
		}
	}

	if len(e.body) > 0 || e.bodyFetched {
		text, encoding := decideBodyEncoding(mimeOf(e), e.body)
		har.Response.Content.Text = text
		har.Response.Content.Encoding = encoding
		har.Response.Content.Size = int64(e.bodyFullSize)
		har.Response.BodySize = e.bodyFullSize
	}
	if e.bodyTruncated {
		har.BodyTruncated = true
	}
	if e.bodyErr != "" {
		har.BodyError = e.bodyErr
	}
	if e.failErr != "" {
		har.Error = e.failErr
	}

	har.Timings = timingsOf(e)
	har.Time = har.Timings.Send + har.Timings.Wait + har.Timings.Receive
	return har
}

func mimeOf(e *entryData) string {
	if e.resp != nil {
		return e.resp.MimeType
	}
	return ""
}

func headerValue(h network.Headers, name string) string {
	for k, v := range h {
		if strings.EqualFold(k, name) {
			if v == nil {
				return ""
			}
			return fmt.Sprint(v)
		}
	}
	return ""
}

// headersToNV converts CDP headers (map[string]any) to sorted name/value pairs.
// Values joined with "\n" (ExtraInfo duplicate-header convention) are split.
func headersToNV(h network.Headers) []HarNV {
	out := []HarNV{}
	if len(h) == 0 {
		return out
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if h[k] == nil {
			continue
		}
		v := fmt.Sprint(h[k])
		for _, part := range strings.Split(v, "\n") {
			out = append(out, HarNV{Name: k, Value: strings.TrimRight(part, "\r")})
		}
	}
	return out
}

// parseQueryString extracts query pairs from a URL, preserving order.
func parseQueryString(rawURL string) []HarNV {
	out := []HarNV{}
	idx := strings.IndexByte(rawURL, '?')
	if idx < 0 {
		return out
	}
	query := rawURL[idx+1:]
	if frag := strings.IndexByte(query, '#'); frag >= 0 {
		query = query[:frag]
	}
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		name, value := pair, ""
		if eq := strings.IndexByte(pair, '='); eq >= 0 {
			name, value = pair[:eq], pair[eq+1:]
		}
		if n, err := url.QueryUnescape(name); err == nil {
			name = n
		}
		if v, err := url.QueryUnescape(value); err == nil {
			value = v
		}
		out = append(out, HarNV{Name: name, Value: value})
	}
	return out
}

// parseCookieHeader parses a request Cookie header ("a=1; b=2") into HAR cookies.
func parseCookieHeader(h string) []HarCookie {
	out := []HarCookie{}
	for _, part := range strings.Split(h, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value := part, ""
		if eq := strings.IndexByte(part, '='); eq >= 0 {
			name, value = strings.TrimSpace(part[:eq]), part[eq+1:]
		}
		out = append(out, HarCookie{Name: name, Value: value})
	}
	return out
}

// parseSetCookies extracts response cookies from Set-Cookie header value(s).
func parseSetCookies(h network.Headers) []HarCookie {
	out := []HarCookie{}
	for k, v := range h {
		if !strings.EqualFold(k, "set-cookie") || v == nil {
			continue
		}
		for _, line := range strings.Split(fmt.Sprint(v), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			nameValue := line
			if sc := strings.IndexByte(line, ';'); sc >= 0 {
				nameValue = line[:sc]
			}
			name, value := nameValue, ""
			if eq := strings.IndexByte(nameValue, '='); eq >= 0 {
				name, value = nameValue[:eq], nameValue[eq+1:]
			}
			out = append(out, HarCookie{Name: strings.TrimSpace(name), Value: value})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// httpVersionOf maps the CDP protocol string to HAR httpVersion.
func httpVersionOf(resp *network.Response) string {
	if resp == nil {
		return "HTTP/1.1"
	}
	switch strings.ToLower(resp.Protocol) {
	case "h2":
		return "h2"
	case "h3", "h3-29", "quic":
		return "h3"
	case "http/1.1", "":
		return "HTTP/1.1"
	case "http/1.0":
		return "HTTP/1.0"
	case "blob", "data", "file":
		return strings.ToLower(resp.Protocol)
	default:
		return resp.Protocol
	}
}

// timingsOf derives HAR timings (ms). ResourceTiming covers send/wait;
// receive comes from the monotonic delta between response and finished events.
func timingsOf(e *entryData) HarTimings {
	var send, wait, receive float64
	if e.resp != nil && e.resp.Timing != nil {
		t := e.resp.Timing
		if t.SendStart >= 0 && t.SendEnd >= t.SendStart {
			send = t.SendEnd - t.SendStart
		}
		if t.SendEnd >= 0 && t.ReceiveHeadersEnd >= t.SendEnd {
			wait = t.ReceiveHeadersEnd - t.SendEnd
		}
	}
	if e.finishedMono != nil && e.respMono != nil {
		delta := e.finishedMono.Time().Sub(e.respMono.Time()).Seconds() * 1000
		if delta > 0 {
			receive = delta
		}
	}
	return HarTimings{Send: send, Wait: wait, Receive: receive}
}

// startedDateTimeOf resolves the request start wall time. WallTime is preferred;
// cdp.MonotonicTime has a boot-time epoch, so only deltas are reliable — hence
// the wallBase + monotonic-delta fallback.
func startedDateTimeOf(e *entryData, cap *capture) time.Time {
	if e.wallStart != nil {
		return e.wallStart.Time()
	}
	if e.monoStart != nil && cap.monoBase != nil {
		return cap.wallBase.Add(e.monoStart.Time().Sub(cap.monoBase.Time()))
	}
	return cap.wallBase
}

func formatHARTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// shouldCaptureBody decides whether a response body is worth fetching.
// Layer 1: resource type; layer 2: binary mime types (XHR-fetches-an-image cases).
func shouldCaptureBody(resType network.ResourceType, mime string) bool {
	switch resType {
	case network.ResourceTypeImage,
		network.ResourceTypeMedia,
		network.ResourceTypeFont,
		network.ResourceTypeStylesheet,
		network.ResourceTypeScript,
		network.ResourceTypeTextTrack,
		network.ResourceTypeManifest,
		network.ResourceTypePing,
		network.ResourceTypeSignedExchange,
		network.ResourceTypeCSPViolationReport,
		network.ResourceTypePreflight,
		network.ResourceTypeWebSocket:
		return false
	}
	m := strings.ToLower(strings.TrimSpace(mime))
	if strings.HasPrefix(m, "image/") || strings.HasPrefix(m, "audio/") ||
		strings.HasPrefix(m, "video/") || strings.HasPrefix(m, "font/") {
		return false
	}
	switch m {
	case "application/octet-stream", "application/zip", "application/gzip",
		"application/x-gzip", "application/wasm", "application/pdf":
		return false
	}
	return true
}

// isTextMime reports whether a mime type usually carries textual content.
func isTextMime(mime string) bool {
	m := strings.ToLower(strings.TrimSpace(mime))
	return strings.HasPrefix(m, "text/") ||
		strings.HasSuffix(m, "+json") || strings.HasSuffix(m, "+xml") ||
		m == "application/json" || m == "application/xml" ||
		m == "application/javascript" || m == "application/x-www-form-urlencoded" ||
		m == ""
}

// decideBodyEncoding renders body bytes as HAR text. Text-like, valid-UTF-8
// bodies stay plain; everything else becomes base64 with encoding set.
func decideBodyEncoding(mime string, data []byte) (text string, encoding string) {
	if isTextMime(mime) && utf8.Valid(data) {
		return string(data), ""
	}
	return base64.StdEncoding.EncodeToString(data), "base64"
}
