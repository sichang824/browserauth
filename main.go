package main

import (
	"os"

	"skills-browserauth/cmd"
)

func main() {
	os.Exit(cmd.Run(os.Args[1:]))
}
