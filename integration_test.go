//go:build unix

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCLIWithEditorServer(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "rmate")
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, background := range []bool{false, true} {
		t.Run(fmt.Sprintf("background=%v", background), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			path := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(path, []byte("before"), 0640); err != nil {
				t.Fatal(err)
			}
			received, finish, done := make(chan error, 1), make(chan struct{}), make(chan error, 1)
			defer close(finish)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					received <- err
					done <- err
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(15 * time.Second))
				io.WriteString(conn, "TextMate test server\n")
				r := bufio.NewReader(conn)
				for i, want := range []string{"before", "stdin"} {
					m, err := readMessage(r)
					if err == nil && (m.Command != "open" || string(m.Data) != want || (i == 0 && (m.Headers["selection"] != "3:2" || m.Headers["display-name"] != "Custom")) || (i == 1 && m.Headers["token"] != "|")) {
						err = fmt.Errorf("unexpected document: %+v", m)
					}
					if err != nil {
						received <- err
						done <- err
						return
					}
				}
				line, err := readLine(r)
				if err == nil && line != "." {
					err = fmt.Errorf("missing request terminator")
				}
				received <- err
				if err != nil {
					done <- err
					return
				}
				<-finish
				_, err = fmt.Fprintf(conn, "save\ntoken: %s\ndata: 5\nafter\nclose\ntoken: %s\n\nsave\ntoken: |\ndata: 6\nedited\nclose\ntoken: |\n\n", path, path)
				if err == nil {
					// Client must finish after all closes even while the server stays open.
					_, err = r.ReadByte()
					if err == io.EOF {
						err = nil
					} else if err == nil {
						err = fmt.Errorf("unexpected client data")
					}
				}
				done <- err
			}()
			outPath := filepath.Join(t.TempDir(), "output")
			output, err := os.Create(outPath)
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
			args := []string{"--host", "127.0.0.1", "-p", port, "--name", "Custom", "-l3:2", path, "-"}
			if !background {
				args = append(args, "--wait")
			}
			cmd := exec.Command(binary, args...)
			cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "RMATE_HOST=127.0.0.1", "RMATE_PORT="+port)
			cmd.Stdin, cmd.Stdout = strings.NewReader("stdin"), output
			var stderr bytes.Buffer
			// Use a file for stderr: a detached worker legitimately keeps it open.
			errFile, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
			if err != nil {
				t.Fatal(err)
			}
			defer errFile.Close()
			cmd.Stderr = errFile
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			select {
			case err := <-received:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				cmd.Process.Kill()
				t.Fatal("no open request")
			}
			if background {
				select {
				case err := <-exited:
					if err != nil {
						t.Fatalf("background parent: %v", err)
					}
				case <-time.After(5 * time.Second):
					cmd.Process.Kill()
					t.Fatal("parent did not return before close")
				}
			} else {
				select {
				case err := <-exited:
					t.Fatalf("wait returned before close: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
			}
			finish <- struct{}{}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("session did not finish")
			}
			if !background {
				select {
				case err := <-exited:
					if err != nil {
						t.Fatalf("foreground: %v", err)
					}
				case <-time.After(5 * time.Second):
					cmd.Process.Kill()
					t.Fatal("wait did not return")
				}
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "after" {
				t.Fatalf("save result: %q, %v", data, err)
			}
			data, err = os.ReadFile(outPath)
			if err != nil || string(data) != "edited" {
				t.Fatalf("stdout: %q, %v", data, err)
			}
			errFile.Seek(0, 0)
			io.Copy(&stderr, errFile)
			if stderr.Len() != 0 {
				t.Fatalf("stderr: %s", &stderr)
			}
		})
	}
	t.Run("connection failure is synchronous", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
		listener.Close()
		cmd := exec.Command(binary, "--host", "127.0.0.1", "-p", port, filepath.Join(t.TempDir(), "new"))
		out, err := cmd.CombinedOutput()
		if err == nil || !bytes.Contains(out, []byte("connect to")) {
			t.Fatalf("expected connection failure: %s, %v", out, err)
		}
	})
}
