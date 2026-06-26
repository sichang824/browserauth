package cmd

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"skills-browserauth/site"
)

const sitesUsage = `browserauth sites - manage site YAML configs

Usage:
  browserauth sites list
  browserauth sites path
  browserauth sites show <id>
  browserauth sites init
  browserauth sites add <id> --from-file PATH [--force]
  browserauth sites remove <id>

Sites directory (default): ~/.browserauth/sites
Override: BROWSERAUTH_SITES_DIR or BROWSERAUTH_DATA_DIR
`

// Sites handles "browserauth sites ..." commands.
func Sites(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, sitesUsage)
		return 2
	}

	switch args[0] {
	case "list":
		return listSites()
	case "path":
		fmt.Println(site.DefaultSitesDir())
		return 0
	case "show":
		return showSite(args[1:])
	case "init":
		return initSites(args[1:])
	case "add":
		return addSite(args[1:])
	case "remove", "rm":
		return removeSite(args[1:])
	case "-h", "--help", "help":
		fmt.Print(sitesUsage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown sites subcommand: %s\n\n", args[0])
		fmt.Fprint(os.Stderr, sitesUsage)
		return 2
	}
}

func listSites() int {
	ids, err := site.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(ids) == 0 {
		fmt.Fprintf(os.Stderr, "No sites in %s\n", site.DefaultSitesDir())
		fmt.Fprintln(os.Stderr, "Add one: browserauth sites add <id> --from-file <yaml>")
		return 0
	}
	for _, id := range ids {
		cfg, err := site.Load(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", id, err)
			continue
		}
		fmt.Printf("%s\t%s\n", id, cfg.ResolvedBaseURL())
	}
	return 0
}

func showSite(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: browserauth sites show <id>")
		return 2
	}
	data, err := site.ReadRaw(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Print(string(data))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		fmt.Println()
	}
	return 0
}

func initSites(_ []string) int {
	dir, err := site.EnsureSitesDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("Sites directory ready: %s\n", dir)
	return 0
}

func addSite(args []string) int {
	fs := flag.NewFlagSet("sites add", flag.ExitOnError)
	fromFile := fs.String("from-file", "", "YAML file to install (required)")
	force := fs.Bool("force", false, "overwrite existing site file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: browserauth sites add <id> --from-file PATH [--force]")
		return 2
	}
	if *fromFile == "" {
		fmt.Fprintln(os.Stderr, "Usage: browserauth sites add <id> --from-file PATH [--force]")
		return 2
	}
	id := strings.TrimSpace(fs.Arg(0))
	path, err := site.Add(id, *fromFile, *force)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("Site %q saved to %s\n", id, path)
	return 0
}

func removeSite(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: browserauth sites remove <id>")
		return 2
	}
	id := strings.TrimSpace(args[0])
	if err := site.Remove(id); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Printf("Removed site %q from %s\n", id, site.DefaultSitesDir())
	return 0
}
