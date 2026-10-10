package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type document struct {
	Token, Path, DisplayName, FileType, Selection string
	Data                                          []byte
	Stdin, Output                                 bool
}

func prepareDocuments(s settings, input io.Reader, inputTerminal, outputPiped bool, stderr io.Writer) ([]document, error) {
	files := s.Files
	if len(files) == 0 {
		if inputTerminal {
			return nil, nil
		}
		files = []string{"-"}
	}
	host, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	var docs []document
	stdinUsed := false
	tokens := make(map[string]bool)
	for i, path := range files {
		d := document{Token: path}
		if path == "-" {
			if stdinUsed {
				return nil, fmt.Errorf("stdin can only be opened once")
			}
			stdinUsed = true
			if inputTerminal {
				fmt.Fprintln(stderr, "Reading stdin; press Ctrl-D to finish")
			}
			d.Stdin, d.Output = true, outputPiped
			d.Token = "-"
			if d.Output {
				d.Token = "|"
			}
			d.DisplayName, d.FileType = host+":untitled (stdin)", "txt"
			d.Data, err = io.ReadAll(input)
		} else {
			d.Path, err = filepath.Abs(path)
			if err != nil {
				return nil, err
			}
			info, statErr := os.Stat(d.Path)
			if statErr != nil && !os.IsNotExist(statErr) {
				return nil, fmt.Errorf("stat %s: %w", path, statErr)
			}
			if statErr == nil {
				if !info.Mode().IsRegular() {
					return nil, fmt.Errorf("%s is not a regular file", path)
				}
				if !s.Force {
					f, openErr := os.OpenFile(d.Path, os.O_WRONLY, 0)
					if f != nil {
						f.Close()
					}
					if openErr != nil || info.Mode().Perm()&0222 == 0 {
						return nil, fmt.Errorf("%s is not writable; use --force to open anyway", path)
					}
				}
				d.Data, err = os.ReadFile(d.Path)
			}
			d.DisplayName = host + ":" + path
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if i < len(s.Names) {
			d.DisplayName = s.Names[i]
		}
		if i < len(s.Types) {
			d.FileType = s.Types[i]
		}
		if i < len(s.Lines) {
			d.Selection = s.Lines[i]
		}
		for _, value := range []string{d.Token, d.Path, d.DisplayName, d.FileType, d.Selection} {
			if strings.ContainsAny(value, "\r\n") {
				return nil, fmt.Errorf("filenames and metadata must not contain newlines")
			}
		}
		if tokens[d.Token] {
			return nil, fmt.Errorf("duplicate document token %q", d.Token)
		}
		tokens[d.Token] = true
		docs = append(docs, d)
	}
	return docs, nil
}

// Write in place to preserve permissions, ownership, symlinks and hard links.
// Keep a private recovery copy until the write, sync and close all succeed.
func saveFile(path string, data []byte) error {
	backup := ""
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		original, err := os.Open(path)
		if err != nil {
			return err
		}
		defer original.Close()
		backup = path + "~"
		for {
			copyFile, err := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if os.IsExist(err) {
				backup += "~"
				continue
			}
			if err != nil {
				return fmt.Errorf("create recovery copy: %w", err)
			}
			_, err = io.Copy(copyFile, original)
			if err == nil {
				err = copyFile.Sync()
			}
			closeErr := copyFile.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				os.Remove(backup)
				return fmt.Errorf("copy original: %w", err)
			}
			break
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err == nil {
		_, err = f.Write(data)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		if backup != "" {
			return fmt.Errorf("%w (original retained in %s)", err, backup)
		}
		return err
	}
	if backup != "" {
		return os.Remove(backup)
	}
	return nil
}
