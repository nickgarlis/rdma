//go:build !linux

package main

import (
	"fmt"
	"os"
	"runtime"
)

func main() {
	fmt.Fprintf(os.Stderr, "setup: not implemented on %s/%s\n", runtime.GOOS, runtime.GOARCH)
	os.Exit(1)
}
