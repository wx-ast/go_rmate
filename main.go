package main

import (
	"fmt"
	"io"
	"os"
)

var version = "1.0.0"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--internal-worker" {
		os.Exit(workerMain())
	}
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin *os.File, stdout, stderr *os.File) int {
	// Parse once without configuration so help also works with broken settings.
	preflight := settings{Host: "localhost", Port: 52698}
	err := preflight.parse(args)
	if err == errHelp {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if err == errVersion {
		fmt.Fprintf(stdout, "rmate-go %s\n", version)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "rmate: %v\n", err)
		return 2
	}
	s, err := loadSettings()
	if err == nil {
		err = s.parse(args)
	}
	if err != nil {
		fmt.Fprintf(stderr, "rmate: %v\n", err)
		return 2
	}
	docs, err := prepareDocuments(s, stdin, isTerminal(stdin), !isTerminal(stdout), stderr)
	if err != nil {
		fmt.Fprintf(stderr, "rmate: %v\n", err)
		return 1
	}
	if len(docs) == 0 {
		return 0
	}
	j := job{Settings: s, Documents: docs}
	if s.Wait {
		err = runSession(j, stdout, stderr, nil)
	} else {
		err = startBackground(j, stdout, stderr)
	}
	if err != nil {
		fmt.Fprintf(stderr, "rmate: %v\n", err)
		return 1
	}
	return 0
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func logf(w io.Writer, enabled bool, format string, args ...any) {
	if enabled {
		fmt.Fprintf(w, format+"\n", args...)
	}
}
