package haropenapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Write creates a new private directory. Existing output is never overwritten.
// Reserving the destination with Mkdir also prevents concurrent exports racing
// to replace each other. Failed exports remove only the directory they created.
func (r *Result) Write(dir string) (err error) {
	if err = os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}
	if err = os.Mkdir(dir, 0o700); err != nil {
		return fmt.Errorf("create output directory (choose a new --output to preserve existing files): %w", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	writeYAML := func(name string, doc obj) error {
		b, e := yaml.Marshal(doc)
		if e != nil {
			return e
		}
		p := filepath.Join(dir, name)
		if e = os.MkdirAll(filepath.Dir(p), 0o700); e != nil {
			return e
		}
		return os.WriteFile(p, b, 0o600)
	}
	if err = writeYAML("openapi.yaml", r.Document); err != nil {
		return err
	}
	manifest := map[string]any{"stats": r.Stats, "files": map[string]string{}, "notes": "Sample-derived OpenAPI 3.1; no live requests. Aggregate merges matching method/path across origins; per-origin files preserve separate contracts."}
	for _, origin := range sortedKeys(r.Hosts) {
		// Origin is parsed HTTP(S); PathEscape encodes colons/IPv6 brackets.
		name := originDir(origin)
		rel := filepath.Join("hosts", name, "openapi.yaml")
		if err = writeYAML(rel, r.Hosts[origin]); err != nil {
			return err
		}
		manifest["files"].(map[string]string)[origin] = filepath.ToSlash(rel)
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), append(b, '\n'), 0o600); err != nil {
		return err
	}
	readme := `# HAR → OpenAPI

openapi.yaml is the aggregate OpenAPI 3.1 draft. hosts/*/openapi.yaml preserves
each HTTP origin separately; manifest.json maps origins to files and reports skips.
When multiple origins share a method/path, the aggregate unions their schemas.

No request/response values, examples, authentication headers or cookie values are
exported. Structured query keys and dynamic object keys are omitted. Numeric,
UUID and long hex path segments are templated. Review other private path segments
and application-defined property names before sharing; use path_templates rules
for site-specific paths. Rule descriptions are user-supplied and copied verbatim.

Schema inference is bounded to 10 levels. Unknown/truncated/binary bodies have
unconstrained schemas. Only path parameters are marked required. API permissions,
business success, signatures, required fields and unobserved cases need review.
This command does not configure authentication or replay any recorded request.
`
	err = os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o600)
	return err
}

func originDir(origin string) string {
	// Hex encoding the authority is unnecessary for normal hosts; escape every
	// non-portable byte so schemes, ports and IPv6 remain distinct on disk.
	var b strings.Builder
	for _, c := range []byte(strings.ReplaceAll(origin, "://", "--")) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "_%02x", c)
		}
	}
	return b.String()
}
