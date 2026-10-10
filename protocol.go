package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type job struct {
	Settings  settings
	Documents []document
}

type message struct {
	Command string
	Headers map[string]string
	Data    []byte
	HasData bool
}

// ReadLine bounds protocol headers without limiting document size.
func readLine(r *bufio.Reader) (string, error) {
	var line []byte
	for {
		part, more, err := r.ReadLine()
		if err != nil {
			return "", err
		}
		line = append(line, part...)
		if len(line) > 64*1024 {
			return "", fmt.Errorf("protocol line exceeds 64 KiB")
		}
		if !more {
			return string(line), nil
		}
	}
}

func readMessage(r *bufio.Reader) (message, error) {
	m := message{Headers: make(map[string]string)}
	cmd, err := readLine(r)
	if err != nil {
		return m, err
	}
	m.Command = strings.TrimSpace(cmd)
	var data bytes.Buffer
	for n := 0; n < 1024; n++ {
		line, err := readLine(r)
		if err != nil {
			return m, fmt.Errorf("incomplete %s message: %w", m.Command, err)
		}
		if strings.TrimSpace(line) == "" {
			m.Data = data.Bytes()
			return m, nil
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return m, fmt.Errorf("invalid protocol header %q", line)
		}
		key, value = strings.TrimSpace(key), strings.TrimPrefix(value, " ")
		if key != "data" {
			m.Headers[key] = value
			continue
		}
		length, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || length < 0 {
			return m, fmt.Errorf("invalid data length %q", value)
		}
		m.HasData = true
		if _, err := io.CopyN(&data, r, length); err != nil {
			return m, fmt.Errorf("incomplete data: %w", err)
		}
	}
	return m, fmt.Errorf("too many protocol headers")
}

func sendDocuments(w io.Writer, docs []document) error {
	b := bufio.NewWriter(w)
	for _, d := range docs {
		fmt.Fprintln(b, "open")
		for _, h := range [][2]string{{"display-name", d.DisplayName}, {"file-type", d.FileType}, {"real-path", d.Path}, {"selection", d.Selection}, {"data-on-save", "yes"}, {"re-activate", "yes"}, {"token", d.Token}} {
			if h[1] != "" {
				fmt.Fprintf(b, "%s: %s\n", h[0], h[1])
			}
		}
		fmt.Fprintf(b, "data: %d\n", len(d.Data))
		if _, err := b.Write(d.Data); err != nil {
			return err
		}
		fmt.Fprintln(b)
	}
	fmt.Fprintln(b, ".")
	return b.Flush()
}

func runSession(j job, stdout, stderr io.Writer, ready func()) error {
	address := net.JoinHostPort(j.Settings.Host, strconv.Itoa(j.Settings.Port))
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", address, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	r := bufio.NewReader(conn)
	hello, err := readLine(r)
	if err != nil {
		return fmt.Errorf("read editor greeting: %w", err)
	}
	if strings.TrimSpace(hello) == "" {
		return fmt.Errorf("empty editor greeting")
	}
	logf(stderr, j.Settings.Verbose, "Connected: %s", hello)
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}
	if err := sendDocuments(conn, j.Documents); err != nil {
		return fmt.Errorf("send files: %w", err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return err
	}
	if ready != nil {
		ready()
	}
	return receiveMessages(r, j.Documents, stdout, stderr, j.Settings.Verbose)
}

func receiveMessages(r *bufio.Reader, docs []document, stdout, stderr io.Writer, verbose bool) error {
	open := make(map[string]*document)
	for i := range docs {
		open[docs[i].Token] = &docs[i]
	}
	var firstSaveError error
	for len(open) > 0 {
		m, err := readMessage(r)
		if err == io.EOF {
			return firstSaveError
		}
		if err != nil {
			return err
		}
		if m.Command == "." {
			continue
		}
		if m.Command != "save" && m.Command != "close" {
			logf(stderr, verbose, "Ignored command: %s", m.Command)
			continue
		}
		d, ok := open[m.Headers["token"]]
		if !ok {
			return fmt.Errorf("%s for unknown token %q", m.Command, m.Headers["token"])
		}
		if m.Command == "save" {
			if !m.HasData {
				return fmt.Errorf("save for %q has no data", d.Token)
			}
			if d.Stdin {
				d.Data = m.Data
			} else {
				logf(stderr, verbose, "Saving %s", d.Path)
				if err := saveFile(d.Path, m.Data); err != nil {
					err = fmt.Errorf("save %s: %w", d.Path, err)
					fmt.Fprintf(stderr, "rmate: %v\n", err)
					if firstSaveError == nil {
						firstSaveError = err
					}
				}
			}
		} else {
			if d.Output {
				if _, err := stdout.Write(d.Data); err != nil {
					return fmt.Errorf("write stdout: %w", err)
				}
			}
			logf(stderr, verbose, "Closed %s", d.Token)
			delete(open, d.Token)
		}
	}
	return firstSaveError
}
