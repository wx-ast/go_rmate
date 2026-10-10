//go:build !unix

package main

import (
	"fmt"
	"os"
)

func startBackground(j job, stdout, stderr *os.File) error {
	return fmt.Errorf("background mode requires Unix; use --wait on this platform")
}

func workerMain() int { return 1 }
