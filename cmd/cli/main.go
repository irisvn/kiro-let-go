package main

import (
	"fmt"
	"os"

	"github.com/irisvn/kiro-let-go/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "[error] %v\n", err)
		os.Exit(1)
	}
}
