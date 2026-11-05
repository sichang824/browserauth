package haropenapi

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testEntry(method, target, body string) entry {
	var e entry
	e.Request.Method = method
	e.Request.URL = target
	e.Response.Status = 200
	e.Response.Content = content{MimeType: "application/json", Text: body}
	return e
}
func runSamples(t *testing.T, c Config, es ...entry) *Result {
	t.Helper()
	b, err := json.Marshal(obj{"log": obj{"entries": es}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Convert(strings.NewReader(string(b)), c)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func opAt(r *Result, path, method string) obj {
	return r.Document["paths"].(obj)[path].(obj)[method].(obj)
}
func responseAt(op obj, status, mime string) obj {
	return op["responses"].(obj)[status].(obj)["content"].(obj)[mime].(obj)["schema"].(obj)
}

func TestMergeOriginsAndMixedResponses(t *testing.T) {
	a := testEntry("GET", "https://a.example/items/123?search=private-query", `{"data":[{"id":1,"name":"private-name"}],"optional":null}`)
	b := testEntry("GET", "https://a.example/items/456", `{"data":[{"id":2,"extra":true}],"optional":"text"}`)
	c := testEntry("GET", "http://a.example:8080/items/789", `{"other":42}`)
	r := runSamples(t, Config{}, a, b, c)
	if r.Stats.Operations != 1 || r.Stats.Hosts != 2 {
		t.Fatal(r.Stats)
	}
	op := opAt(r, "/items/{id1}", "get")
	if len(op["servers"].([]any)) != 2 || op["x-merged-origins"] != true {
		t.Fatal("origins overwritten")
	}
	s := responseAt(op, "200", "application/json")
	ps := s["properties"].(obj)
	if ps["data"] == nil || ps["other"] == nil || ps["optional"].(obj)["anyOf"] == nil {
		t.Fatal("response union lost")
	}
	item := ps["data"].(obj)["items"].(obj)["properties"].(obj)
	if item["name"] == nil || item["extra"] == nil {
		t.Fatal("array items lost")
	}
	for _, p := range op["parameters"].([]any) {
		v := p.(obj)
		if v["required"] != (v["in"] == "path") {
			t.Fatal("inferred required field")
		}
	}
	encoded, _ := r.JSON()
	for _, v := range []string{"private-query", "private-name", "/123", "/456", "/789"} {
		if strings.Contains(string(encoded), v) {
			t.Fatal("sample value leaked")
		}
	}
	// Host-specific output must not acquire another origin's response fields.
	h := r.Hosts["https://a.example"]["paths"].(obj)["/items/{id1}"].(obj)["get"].(obj)
	if responseAt(h, "200", "application/json")["properties"].(obj)["other"] != nil {
		t.Fatal("cross-origin pollution")
	}
}

func TestPrivacyAndStructuredQuery(t *testing.T) {
	e := testEntry("POST", "https://api.example/session-cookie-secret", `{"normal":"private-response","123456789":{"name":"person"}}`)
	e.Request.Headers = []pair{{Name: "Cookie", Value: "sid=session-cookie-secret"}, {Name: "Authorization", Value: "Bearer bearer-secret-value"}}
	e.Request.Query = []pair{{Name: `{"token":"query-secret"}`, Value: ""}, {Name: "token", Value: "query-secret"}, {Name: "normal", Value: "private-query"}}
	e.Request.PostData = &postData{MimeType: "application/json", Text: `{"password":"password-secret","payload":{"name":"private-person"}}`}
	r := runSamples(t, Config{}, e)
	op := opAt(r, "/{id1}", "post")
	if op["x-omitted-structured-query"] != true {
		t.Fatal("structured query not marked")
	}
	b, _ := r.JSON()
	for _, secret := range []string{"session-cookie-secret", "bearer-secret-value", "query-secret", "private-query", "password-secret", "private-response", "private-person", "123456789"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("private value leaked (%d bytes)", len(secret))
		}
	}
	if responseAt(op, "200", "application/json")["additionalProperties"] != true {
		t.Fatal("dynamic keys must remain a dictionary")
	}
}

func TestBodiesAndRepeatedParameters(t *testing.T) {
	a := testEntry("POST", "https://example/api.json?tag=a&tag=b", `null`)
	a.Request.PostData = &postData{MimeType: "application/x-www-form-urlencoded; charset=utf-8", Text: "message=hello&tag=a&tag=b"}
	b := testEntry("POST", "https://example/upload", `{"ok":true}`)
	b.Request.PostData = &postData{MimeType: "multipart/form-data; boundary=abc", Params: []pair{{Name: "upload", FileName: "private-file.txt"}, {Name: "description", Value: "private-text"}}}
	b.Response.Content.Text = base64.StdEncoding.EncodeToString([]byte(b.Response.Content.Text))
	b.Response.Content.Encoding = "base64"
	r := runSamples(t, Config{}, a, b)
	op := opAt(r, "/api.json", "post")
	if responseAt(op, "200", "application/json")["type"] != "null" {
		t.Fatal("JSON null lost")
	}
	p := op["parameters"].([]any)[0].(obj)
	if p["schema"].(obj)["type"] != "array" || p["explode"] != true {
		t.Fatal("repeated query lost")
	}
	form := op["requestBody"].(obj)["content"].(obj)["application/x-www-form-urlencoded"].(obj)["schema"].(obj)["properties"].(obj)
	if form["tag"].(obj)["type"] != "array" {
		t.Fatal("repeated form lost")
	}
	upload := opAt(r, "/upload", "post")
	if responseAt(upload, "200", "application/json")["type"] != "object" {
		t.Fatal("base64 JSON not decoded")
	}
	file := upload["requestBody"].(obj)["content"].(obj)["multipart/form-data"].(obj)["schema"].(obj)["properties"].(obj)["upload"].(obj)
	if file["format"] != "binary" {
		t.Fatal("multipart file type lost")
	}
}

func TestUnknownAndTruncatedResponses(t *testing.T) {
	a := testEntry("GET", "https://example/api", `{"ok":true}`)
	b := testEntry("GET", "https://example/api", `{"partial":`)
	b.Truncated = true
	c := testEntry("GET", "https://example/api", "")
	c.Response.Status = 0
	d := testEntry("GET", "https://example/api", "")
	d.Response.Status = 204
	r := runSamples(t, Config{}, a, b, c, d)
	op := opAt(r, "/api", "get")
	if len(responseAt(op, "200", "application/json")) != 0 {
		t.Fatal("truncated response falsely constrained")
	}
	if op["x-incomplete-response-body"] != true {
		t.Fatal("missing truncation marker")
	}
	rs := op["responses"].(obj)
	if rs["default"] == nil || rs["204"].(obj)["content"] != nil {
		t.Fatal("invalid empty response")
	}
}

func TestRulesAndFiltering(t *testing.T) {
	c, err := ReadConfig(strings.NewReader(`title: Example
include_hosts: ["*.example.com"]
exclude_paths: ['^/health$']
path_templates:
  - host: api.example.com
    pattern: '^/users/[^/]+$'
    template: /users/{username}
operations:
  - host: api.example.com
    method: GET
    path: /users/{username}
    summary: User profile
    tags: [Users]
`))
	if err != nil {
		t.Fatal(err)
	}
	api := testEntry("GET", "https://api.example.com/users/private-slug", `{"id":1}`)
	static := testEntry("GET", "https://api.example.com/logo.png", "")
	static.ResourceType = "Image"
	page := testEntry("GET", "https://api.example.com/", "")
	page.Response.Content.MimeType = "text/html"
	preflight := testEntry("OPTIONS", "https://api.example.com/users/private-slug", "")
	r := runSamples(t, c, api, static, page, preflight, testEntry("GET", "https://other.test/api", "{}"), testEntry("GET", "https://api.example.com/health", "{}"))
	if r.Stats.Included != 1 || r.Stats.Entries != 6 {
		t.Fatal(r.Stats)
	}
	if opAt(r, "/users/{username}", "get")["summary"] != "User profile" {
		t.Fatal("annotation missing")
	}
	for _, bad := range []string{"include_host: [api.example.com]", "exclude_paths: ['[']", "path_templates: [{pattern: x, template: '/x/{bad}/{bad}'}]", "title: A\n---\ntitle: B"} {
		if _, err := ReadConfig(strings.NewReader(bad)); err == nil {
			t.Fatal("bad rules accepted")
		}
	}
}

func TestMalformedAndEmptyHAR(t *testing.T) {
	for _, s := range []string{"{}", "null", `{"log":{}}`, `{"log":{"entries":null}}`, `{"log":{"entries":[]}}`, `{"log":{"entries":[]}} {}`, "broken"} {
		if _, err := Convert(strings.NewReader(s), Config{}); err == nil {
			t.Fatal("invalid or empty HAR accepted")
		}
	}
}

func TestEquivalentTemplatesHaveOnePath(t *testing.T) {
	c := Config{PathTemplates: []PathTemplate{
		{Host: "a.example", Pattern: "^/users/[^/]+$", Template: "/users/{username}"},
		{Host: "b.example", Pattern: "^/users/[^/]+$", Template: "/users/{account}"},
	}, Operations: []OperationRule{{Host: "a.example", Path: "/users/{username}", Summary: "User lookup"}}}
	r := runSamples(t, c, testEntry("GET", "https://a.example/users/alice", "{}"), testEntry("GET", "https://b.example/users/bob", "{}"))
	if len(r.Document["paths"].(obj)) != 1 {
		t.Fatal("equivalent templates split into invalid OpenAPI paths")
	}
	op := opAt(r, "/users/{account}", "get")
	if op["summary"] != "User lookup" {
		t.Fatal("annotation lost when canonicalizing parameter names")
	}
	ps := op["parameters"].([]any)
	if len(ps) != 1 || ps[0].(obj)["name"] != "account" {
		t.Fatal("path parameter name not canonicalized")
	}
}

func TestStableExportAndNoOverwrite(t *testing.T) {
	e := testEntry("GET", "https://example:8443/api", `{"a":1}`)
	r := runSamples(t, Config{}, e)
	other := runSamples(t, Config{}, e)
	a, _ := r.JSON()
	b, _ := other.JSON()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("unstable output")
	}
	dir := filepath.Join(t.TempDir(), "result")
	if err := r.Write(dir); err != nil {
		t.Fatal(err)
	}
	if err := r.Write(dir); err == nil {
		t.Fatal("existing directory overwritten")
	}
	if _, err := os.Stat(filepath.Join(dir, "openapi.yaml")); err != nil {
		t.Fatal("existing result removed")
	}
	if info, err := os.Stat(filepath.Join(dir, "openapi.yaml")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("output permissions")
	}
	if originDir("http://example:443") == originDir("https://example:443") {
		t.Fatal("origin filenames collide")
	}
}
