package cmd

// parseIsolatedProfileFlag extracts --isolated-profile from login/browser args.
func parseIsolatedProfileFlag(args []string) (rest []string, isolated bool) {
	rest = args
	for len(rest) > 0 {
		switch rest[0] {
		case "--isolated-profile":
			isolated = true
			rest = rest[1:]
		default:
			return rest, isolated
		}
	}
	return rest, isolated
}
