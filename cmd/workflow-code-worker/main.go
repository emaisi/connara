package main

import (
	"apihub-go/internal/codeworker"
	"os"
)

func main() {
	if err := codeworker.Run(os.Stdin, os.Stdout); err != nil {
		os.Exit(1)
	}
}
