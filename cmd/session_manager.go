package cmd

import (
	"fmt"
	"os"
	"time"

	"skills-browserauth/sessionmanager"
)

func internalSessionManager(args []string) int {
	if len(args) != 1 || args[0] != "serve" {
		fmt.Fprintln(os.Stderr, "internal session manager command")
		return 2
	}
	if err := sessionmanager.NewServer().Serve(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func SessionManager(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: browserauth session status|stop")
		return 2
	}
	switch args[0] {
	case "status":
		response, err := sessionmanager.Call(sessionmanager.Request{Action: "status"})
		if err != nil {
			fmt.Fprintln(os.Stderr, "no retained browser session")
			return 1
		}
		for _, item := range response.Sessions {
			fmt.Printf("%s requests=%d age=%s idle=%s\n", item.Site, item.RequestCount, time.Since(item.CreatedAt).Round(time.Second), time.Since(item.LastUsed).Round(time.Second))
		}
		return 0
	case "stop":
		if _, err := sessionmanager.Call(sessionmanager.Request{Action: "shutdown"}); err != nil {
			fmt.Fprintln(os.Stderr, "no retained browser session")
			return 1
		}
		fmt.Println("retained browser sessions stopped")
		return 0
	default:
		fmt.Fprintln(os.Stderr, "Usage: browserauth session status|stop")
		return 2
	}
}
