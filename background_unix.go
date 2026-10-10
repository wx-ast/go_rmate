//go:build unix

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Re-exec instead of forking the Go runtime. The parent waits for the worker
// to send the documents so connection errors are reported synchronously.
func startBackground(j job, stdout, stderr *os.File) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	input, err := os.CreateTemp("", "rmate-job-*")
	if err != nil {
		return err
	}
	defer input.Close()
	defer os.Remove(input.Name())
	if err := json.NewEncoder(input).Encode(j); err != nil {
		return err
	}
	if _, err := input.Seek(0, 0); err != nil {
		return err
	}
	status, signal, err := os.Pipe()
	if err != nil {
		return err
	}
	defer status.Close()
	defer signal.Close()
	cmd := exec.Command(executable, "--internal-worker")
	cmd.ExtraFiles = []*os.File{signal, input}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start background worker: %w", err)
	}
	signal.Close()
	// The child has an open descriptor; remove sensitive data from the directory.
	os.Remove(input.Name())
	line, err := bufio.NewReader(status).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "ready" {
		cmd.Wait()
		if err != nil {
			return fmt.Errorf("background startup: %w", err)
		}
		return fmt.Errorf("background startup: %s", strings.TrimSpace(line))
	}
	return cmd.Process.Release()
}

func workerMain() int {
	signal := os.NewFile(3, "ready")
	input := os.NewFile(4, "job")
	defer signal.Close()
	defer input.Close()
	var j job
	if err := json.NewDecoder(input).Decode(&j); err != nil {
		fmt.Fprintf(signal, "%v\n", err)
		return 1
	}
	input.Close()
	started := false
	err := runSession(j, os.Stdout, os.Stderr, func() {
		started = true
		fmt.Fprintln(signal, "ready")
		signal.Close()
	})
	if err != nil {
		if !started {
			fmt.Fprintf(signal, "%v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "rmate: %v\n", err)
		}
		return 1
	}
	return 0
}
