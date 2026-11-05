package cmd

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"skills-browserauth/haropenapi"
	"skills-browserauth/store"
)

// OpenAPI converts a HAR without opening Chrome, requiring a key, or making HTTP requests.
func OpenAPI(siteID string, args []string) int {
	flags := flag.NewFlagSet("openapi", flag.ContinueOnError)
	input := flags.String("input", "", "HAR file (site command defaults to latest recording)")
	output := flags.String("output", "", "new output directory; never overwrites existing files")
	rules := flags.String("config", "", "optional conversion rules YAML")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || (*input == "" && siteID == "") {
		fmt.Fprintln(os.Stderr, "Usage: browserauth [<site>] openapi [--input recording.har] [--output NEW_DIR] [--config rules.yaml]")
		return 2
	}
	if *input == "" {
		var err error
		*input, err = latestRecording(siteID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	config := haropenapi.Config{}
	if *rules == "" && siteID != "" {
		candidate := filepath.Join(store.DefaultDataDir(), "openapi-rules", siteID+".yaml")
		if _, err := os.Stat(candidate); err == nil {
			*rules = candidate
		} else if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	if *rules != "" {
		f, err := os.Open(*rules)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		config, err = haropenapi.ReadConfig(f)
		f.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	f, err := os.Open(*input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	res, err := haropenapi.Convert(f, config)
	f.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *output == "" {
		base := filepath.Join(store.DefaultDataDir(), "openapi")
		if siteID != "" {
			base = filepath.Join(base, siteID)
		}
		*output = filepath.Join(base, strings.TrimSuffix(filepath.Base(*input), filepath.Ext(*input))+"-openapi")
	}
	if err = res.Write(*output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	abs, err := filepath.Abs(*output)
	if err != nil {
		abs = *output
	}
	fmt.Fprintf(os.Stderr, "Converted %d/%d entries into %d operations across %d origins.\n", res.Stats.Included, res.Stats.Entries, res.Stats.Operations, res.Stats.Hosts)
	fmt.Fprintln(os.Stdout, filepath.Join(abs, "openapi.yaml"))
	return 0
}

func latestRecording(siteID string) (string, error) {
	es, err := os.ReadDir(store.RecordingsDir())
	if err != nil {
		return "", err
	}
	var best string
	var newest int64
	for _, e := range es {
		// Do not use a glob: site IDs must be matched literally.
		if e.IsDir() || !strings.HasPrefix(e.Name(), siteID+"-") || !strings.HasSuffix(e.Name(), ".har") {
			continue
		}
		stamp := strings.TrimSuffix(strings.TrimPrefix(e.Name(), siteID+"-"), ".har")
		if _, err := time.Parse("20060102-150405", stamp); err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return "", err
		}
		t := info.ModTime().UnixNano()
		if best == "" || t > newest || t == newest && e.Name() > filepath.Base(best) {
			best = filepath.Join(store.RecordingsDir(), e.Name())
			newest = t
		}
	}
	if best == "" {
		return "", fmt.Errorf("no recording for site %q; run browserauth %s record or pass --input", siteID, siteID)
	}
	return best, nil
}
