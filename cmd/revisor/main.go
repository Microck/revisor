package main

import (
	"context"
	"fmt"
	"os"

	"github.com/microck/revisor/internal/revisor"
)

func main() {
	code, err := revisor.Main(context.Background(), os.Args[1:], revisor.SystemRuntime())
	if err != nil {
		fmt.Fprintf(os.Stderr, "revisor: %v\n", err)
		fmt.Fprintln(os.Stderr, "Run `revisor --help` for usage.")
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
