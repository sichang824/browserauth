package haropenapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
)

// These input types intentionally support both BrowserAuth and standard browser
// HAR exports (including params-only multipart/form data).
type pair struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	FileName string `json:"fileName"`
}
type postData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Params   []pair `json:"params"`
}
type content struct {
	MimeType  string `json:"mimeType"`
	Text      string `json:"text"`
	Encoding  string `json:"encoding"`
	Truncated bool   `json:"_bodyTruncated"`
}
type entry struct {
	Request struct {
		Method   string    `json:"method"`
		URL      string    `json:"url"`
		Headers  []pair    `json:"headers"`
		Cookies  []pair    `json:"cookies"`
		Query    []pair    `json:"queryString"`
		PostData *postData `json:"postData"`
	} `json:"request"`
	Response struct {
		Status  int     `json:"status"`
		Content content `json:"content"`
		Headers []pair  `json:"headers"`
		Cookies []pair  `json:"cookies"`
	} `json:"response"`
	ResourceType string `json:"_resourceType"`
	Truncated    bool   `json:"_bodyTruncated"`
	BodyError    string `json:"_bodyError"`
}

type Result struct {
	Document obj
	Hosts    map[string]obj // keyed by origin, including scheme and port
	Stats    Stats
}
type Stats struct {
	Entries    int            `json:"entries"`
	Included   int            `json:"included_entries"`
	Operations int            `json:"operations"`
	Hosts      int            `json:"origins"`
	Skipped    map[string]int `json:"skipped"`
}
type sample struct {
	e                          entry
	origin, host, path, method string
	rulePath                   string
}

// Convert never performs network requests or copies sample values into the
// schemas. Path segments are normalized conservatively; unknown application
// identifiers in paths/property names still need review before sharing.
func Convert(r io.Reader, c Config) (*Result, error) {
	if err := c.prepare(); err != nil {
		return nil, err
	}
	var har struct {
		Log *struct {
			Entries *[]entry `json:"entries"`
		} `json:"log"`
	}
	d := json.NewDecoder(r)
	if err := d.Decode(&har); err != nil {
		return nil, fmt.Errorf("invalid HAR JSON")
	}
	var extra any
	if d.Decode(&extra) != io.EOF || har.Log == nil || har.Log.Entries == nil {
		return nil, fmt.Errorf("expected one HAR document with log.entries")
	}
	es := *har.Log.Entries
	secrets := collectSecrets(es)
	res := &Result{Hosts: map[string]obj{}, Stats: Stats{Entries: len(es), Skipped: map[string]int{}}}
	all := []sample{}
	byOrigin := map[string][]sample{}
	for _, e := range es {
		u, err := url.Parse(e.Request.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
			res.Stats.Skipped["invalid_or_non_http_url"]++
			continue
		}
		h := strings.ToLower(u.Hostname())
		m := strings.ToLower(e.Request.Method)
		if !validMethod(m) || m == "options" {
			res.Stats.Skipped["preflight_or_unsupported_method"]++
			continue
		}
		p := u.EscapedPath()
		if p == "" {
			p = "/"
		}
		if len(c.IncludeHosts) > 0 && !hostIn(c.IncludeHosts, h) || hostIn(c.ExcludeHosts, h) || len(c.IncludePaths) > 0 && !pathIn(c.IncludePaths, p) || pathIn(c.ExcludePaths, p) {
			res.Stats.Skipped["rules"]++
			continue
		}
		if !c.IncludeStatic && isStatic(e, p) {
			res.Stats.Skipped["static_or_document"]++
			continue
		}
		p = templatePath(p, h, c, secrets)
		origin := strings.ToLower(u.Scheme + "://" + u.Host)
		s := sample{e: e, origin: origin, host: h, path: p, method: m, rulePath: p}
		all = append(all, s)
		byOrigin[origin] = append(byOrigin[origin], s)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("no matching HTTP API entries; adjust host/path filters or include_static")
	}
	title := c.Title
	if title == "" {
		title = "Recorded HTTP APIs"
	}
	res.Document = buildDocument(all, title, c, secrets)
	for origin, ss := range byOrigin {
		res.Hosts[origin] = buildDocument(ss, title+" — "+origin, c, secrets)
	}
	res.Stats.Included = len(all)
	res.Stats.Hosts = len(byOrigin)
	for _, pi := range res.Document["paths"].(obj) {
		res.Stats.Operations += len(pi.(obj))
	}
	return res, nil
}

func isStatic(e entry, p string) bool {
	switch strings.ToLower(e.ResourceType) {
	case "document", "image", "media", "font", "stylesheet", "script", "manifest", "websocket", "preflight":
		return true
	}
	mt := mediaType(e.Response.Content.MimeType)
	if strings.HasPrefix(mt, "image/") || strings.HasPrefix(mt, "audio/") || strings.HasPrefix(mt, "video/") || strings.HasPrefix(mt, "font/") {
		return true
	}
	switch mt {
	case "text/html", "text/css", "text/javascript", "application/javascript", "application/wasm":
		return true
	}
	// JSON URLs can be business APIs; distinguishing static JSON configuration
	// requires optional host/path filters instead of excluding every .json URL.
	switch strings.ToLower(path.Ext(p)) {
	case ".js", ".css", ".map", ".png", ".jpg", ".jpeg", ".webp", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".mp4", ".m4s", ".ts", ".mp3", ".wasm", ".zip":
		return true
	}
	return false
}

func templatePath(p, host string, c Config, secrets map[string]bool) string {
	for _, t := range c.PathTemplates {
		if (t.Host == "" || hostMatches(t.Host, host)) && t.re.MatchString(p) {
			return t.Template
		}
	}
	parts := strings.Split(p, "/")
	n := 0
	for i, s := range parts {
		decoded, err := url.PathUnescape(s)
		if err != nil {
			decoded = s
		}
		private := dynamicID.MatchString(decoded) || strings.ContainsAny(decoded, "@{}")
		for secret := range secrets {
			if strings.Contains(decoded, secret) {
				private = true
				break
			}
		}
		if private {
			n++
			parts[i] = "{id" + strconv.Itoa(n) + "}"
		}
	}
	return strings.Join(parts, "/")
}

func buildDocument(ss []sample, title string, c Config, secrets map[string]bool) obj {
	doc := obj{"openapi": "3.1.0", "info": obj{"title": title, "version": "1.0.0", "description": "Inferred from HAR samples, not an official API contract. Values, examples and authentication headers are omitted. Required fields, permissions, signing rules and unobserved responses are unknown. Object/array inference is bounded to 10 levels."}, "paths": obj{}}
	groups := map[string][]sample{}
	origins := map[string]bool{}
	// OpenAPI cannot contain /items/{id} and /items/{name} as distinct paths.
	canonical := map[string]string{}
	for _, s := range ss {
		shape := variable.ReplaceAllString(s.path, "{}")
		if old, ok := canonical[shape]; !ok || s.path < old {
			canonical[shape] = s.path
		}
	}
	for _, s := range ss {
		s.path = canonical[variable.ReplaceAllString(s.path, "{}")]
		groups[s.method+" "+s.path] = append(groups[s.method+" "+s.path], s)
		origins[s.origin] = true
	}
	doc["servers"] = serverList(origins)
	paths := doc["paths"].(obj)
	for _, key := range sortedKeys(groups) {
		group := groups[key]
		s := group[0]
		op := operation(group, c, secrets)
		pi, ok := paths[s.path].(obj)
		if !ok {
			pi = obj{}
			paths[s.path] = pi
		}
		pi[s.method] = op
	}
	return doc
}

func operation(ss []sample, c Config, secrets map[string]bool) obj {
	s := ss[0]
	sum := sha256.Sum256([]byte(s.method + " " + s.path))
	op := obj{"operationId": s.method + "_" + hex.EncodeToString(sum[:8]), "summary": strings.ToUpper(s.method) + " " + s.path, "description": "Observed samples only; parameter presence does not prove that a field is required. Authentication requirements have not been inferred.", "x-observed-count": len(ss), "responses": obj{}}
	origins := map[string]bool{}
	params := map[string]bool{}
	repeatedQuery := map[string]bool{}
	bodies := obj{}
	tags := map[string]bool{}
	for _, v := range variable.FindAllStringSubmatch(s.path, -1) {
		params["path:"+v[1]] = true
	}
	for _, s := range ss {
		origins[s.origin] = true
		queryCounts := map[string]int{}
		for _, nv := range queryPairs(s.e) {
			if safeField(nv.Name, secrets) {
				params["query:"+nv.Name] = true
				queryCounts[nv.Name]++
				if queryCounts[nv.Name] > 1 {
					repeatedQuery[nv.Name] = true
				}
			} else {
				op["x-omitted-structured-query"] = true
			}
		}
		if pd := s.e.Request.PostData; pd != nil {
			mt := mediaType(pd.MimeType)
			sc := requestSchema(pd, secrets)
			old, _ := bodies[mt].(obj)
			bodies[mt] = merge(old, sc)
		}
		status := strconv.Itoa(s.e.Response.Status)
		if s.e.Response.Status < 100 || s.e.Response.Status > 599 {
			status = "default"
		}
		rs := op["responses"].(obj)
		r, ok := rs[status].(obj)
		if !ok {
			r = obj{"description": "Observed HTTP " + status}
			rs[status] = r
		}
		if status == "default" {
			r["description"] = "No valid HTTP status captured (for example a cancelled request)."
		}
		if status != "default" && s.e.Response.Status != 204 && s.e.Response.Status != 304 && s.method != "head" {
			mt := mediaType(s.e.Response.Content.MimeType)
			contents, ok := r["content"].(obj)
			if !ok {
				contents = obj{}
				r["content"] = contents
			}
			ct, ok := contents[mt].(obj)
			sc := responseSchema(s.e, secrets)
			if ok {
				sc = merge(ct["schema"].(obj), sc)
			}
			contents[mt] = obj{"schema": sc}
		}
		if s.e.Truncated || s.e.Response.Content.Truncated || s.e.BodyError != "" {
			op["x-incomplete-response-body"] = true
		}
		for _, r := range c.Operations {
			if r.Path == s.rulePath && (r.Host == "" || hostMatches(r.Host, s.host)) && (r.Method == "" || strings.EqualFold(r.Method, s.method)) {
				if r.Summary != "" {
					op["summary"] = r.Summary
				}
				if r.Description != "" {
					op["description"] = r.Description
				}
				for _, t := range r.Tags {
					tags[t] = true
				}
			}
		}
	}
	op["servers"] = serverList(origins)
	if len(origins) > 1 {
		op["x-merged-origins"] = true
	}
	if len(tags) > 0 {
		op["tags"] = sortedKeys(tags)
	}
	if len(params) > 0 {
		ps := []any{}
		for _, key := range sortedKeys(params) {
			loc, n, _ := strings.Cut(key, ":")
			p := obj{"name": n, "in": loc, "required": loc == "path", "schema": obj{"type": "string"}}
			if loc == "query" {
				p["description"] = "Observed query field; requiredness unknown. Values omitted."
				if repeatedQuery[n] {
					p["schema"] = obj{"type": "array", "items": obj{"type": "string"}}
					p["style"] = "form"
					p["explode"] = true
				}
			}
			ps = append(ps, p)
		}
		op["parameters"] = ps
	}
	if len(bodies) > 0 {
		ct := obj{}
		for mt, sc := range bodies {
			ct[mt] = obj{"schema": sc}
		}
		op["requestBody"] = obj{"required": false, "content": ct}
	}
	return op
}

func mediaType(s string) string {
	m, _, err := mime.ParseMediaType(s)
	if err != nil || !strings.Contains(m, "/") {
		return "application/octet-stream"
	}
	return strings.ToLower(m)
}
func decodeJSON(text string) (any, bool) {
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	var v, extra any
	if d.Decode(&v) != nil || d.Decode(&extra) != io.EOF {
		return nil, false
	}
	return v, true
}
func requestSchema(pd *postData, secrets map[string]bool) obj {
	mt := mediaType(pd.MimeType)
	if mt == "application/json" || strings.HasSuffix(mt, "+json") {
		if v, ok := decodeJSON(pd.Text); ok {
			return infer(v, 0, secrets)
		}
		return obj{}
	}
	if mt == "application/x-www-form-urlencoded" || mt == "multipart/form-data" {
		pairs := pd.Params
		if len(pairs) == 0 && mt == "application/x-www-form-urlencoded" {
			q, err := url.ParseQuery(pd.Text)
			if err == nil {
				for n, vs := range q {
					for _, v := range vs {
						pairs = append(pairs, pair{Name: n, Value: v})
					}
				}
			}
		}
		if len(pairs) == 0 {
			return obj{}
		}
		ps := obj{}
		for _, nv := range pairs {
			if !safeField(nv.Name, secrets) {
				continue
			}
			sc := obj{"type": "string"}
			if nv.FileName != "" {
				sc["format"] = "binary"
			}
			if old, ok := ps[nv.Name].(obj); ok {
				if old["type"] == "array" {
					old["items"] = merge(old["items"].(obj), sc)
				} else {
					ps[nv.Name] = obj{"type": "array", "items": merge(old, sc)}
				}
			} else {
				ps[nv.Name] = sc
			}
		}
		return obj{"type": "object", "properties": ps, "additionalProperties": true}
	}
	return obj{} // raw/unavailable bodies cannot establish their wire schema
}
func responseSchema(e entry, secrets map[string]bool) obj {
	c := e.Response.Content
	if e.Truncated || c.Truncated || e.BodyError != "" || c.Text == "" {
		return obj{}
	}
	text := c.Text
	if c.Encoding == "base64" {
		b, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return obj{}
		}
		text = string(b)
	} else if c.Encoding != "" {
		return obj{}
	}
	if v, ok := decodeJSON(text); ok {
		return infer(v, 0, secrets)
	}
	return obj{}
}
func queryPairs(e entry) []pair {
	if len(e.Request.Query) > 0 {
		return e.Request.Query
	}
	u, err := url.Parse(e.Request.URL)
	if err != nil {
		return nil
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return []pair{{Name: "<unparsed>"}}
	}
	p := []pair{}
	for n, vs := range q {
		for _, v := range vs {
			p = append(p, pair{Name: n, Value: v})
		}
	}
	return p
}
func collectSecrets(es []entry) map[string]bool {
	ss := map[string]bool{}
	add := func(s string) {
		if len(s) >= 8 {
			ss[s] = true
		}
	}
	for _, e := range es {
		for _, p := range append(append([]pair{}, e.Request.Cookies...), e.Response.Cookies...) {
			add(p.Value)
		}
		for _, h := range append(append([]pair{}, e.Request.Headers...), e.Response.Headers...) {
			switch strings.ToLower(h.Name) {
			case "cookie", "set-cookie":
				for _, p := range strings.Split(h.Value, ";") {
					_, v, ok := strings.Cut(strings.TrimSpace(p), "=")
					if ok {
						add(v)
					}
				}
			case "authorization", "x-csrf-token", "x-api-key":
				add(h.Value)
				_, v, ok := strings.Cut(h.Value, " ")
				if ok {
					add(v)
				}
			}
		}
		pairs := queryPairs(e)
		if pd := e.Request.PostData; pd != nil {
			pairs = append(pairs, pd.Params...)
			if q, err := url.ParseQuery(pd.Text); err == nil {
				for n, vs := range q {
					for _, v := range vs {
						pairs = append(pairs, pair{Name: n, Value: v})
					}
				}
			}
		}
		for _, p := range pairs {
			n := strings.ToLower(p.Name)
			for _, s := range []string{"password", "token", "csrf", "secret", "captcha", "qrcode", "phone", "tel", "message"} {
				if strings.Contains(n, s) {
					add(p.Value)
					break
				}
			}
		}
	}
	return ss
}
func sortedKeys[T any](m map[string]T) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
func serverList(origins map[string]bool) []any {
	s := []any{}
	for _, o := range sortedKeys(origins) {
		s = append(s, obj{"url": o})
	}
	return s
}

// JSON serializes the aggregate document for embedding or external validators.
func (r *Result) JSON() ([]byte, error) { return json.Marshal(r.Document) }
