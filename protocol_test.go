package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtocolBinaryAndChunks(t *testing.T) {
	payload := []byte("Привет\x00\xff\nclose\ntoken: injected\n")
	var wire bytes.Buffer
	docs := []document{{Token: "file", Path: "/tmp/file", DisplayName: "name: with colon", Data: payload}, {Token: "new", DisplayName: "empty"}}
	if err := sendDocuments(&wire, docs); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(&wire)
	for _, d := range docs {
		m, err := readMessage(r)
		if err != nil {
			t.Fatal(err)
		}
		if m.Command != "open" || m.Headers["token"] != d.Token || !m.HasData || !bytes.Equal(m.Data, d.Data) {
			t.Fatalf("bad open: %+v", m)
		}
	}
	if line, err := readLine(r); err != nil || line != "." {
		t.Fatalf("missing terminator: %q, %v", line, err)
	}
	m, err := readMessage(bufio.NewReader(strings.NewReader("save\ntoken: file \ndata: 2\nabdata: 3\ncde\n")))
	if err != nil || string(m.Data) != "abcde" || m.Headers["token"] != "file " {
		t.Fatalf("chunks or whitespace corrupted: %+v, %v", m, err)
	}
	for _, wire := range []string{"save\ndata: -1\n\n", "save\ndata: nope\n\n", "save\ndata: 5\nabc", "save\ninvalid\n\n", "save\ntoken: file\n", strings.Repeat("x", 65537) + "\n"} {
		if _, err := readMessage(bufio.NewReader(strings.NewReader(wire))); err == nil {
			t.Errorf("accepted malformed message %.40q", wire)
		}
	}
}

func TestStdinClosesWithItsOwnDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	docs := []document{{Token: "file", Path: path}, {Token: "|", Stdin: true, Output: true, Data: []byte("original")}}
	wire := "save\ntoken: file\ndata: 0\n\nclose\ntoken: file\n\nsave\ntoken: |\ndata: 3\nnew\nclose\ntoken: |\n\n"
	var out bytes.Buffer
	if err := receiveMessages(bufio.NewReader(strings.NewReader(wire)), docs, &out, io.Discard, false); err != nil {
		t.Fatal(err)
	}
	if out.String() != "new" {
		t.Fatalf("stdin output: %q", out.String())
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) != 0 {
		t.Fatalf("empty file save: %q, %v", data, err)
	}
	// A server cannot choose an arbitrary destination outside the opened files.
	wire = fmt.Sprintf("save\ntoken: %s\ndata: 3\nbad\n", filepath.Join(dir, "other"))
	if err := receiveMessages(bufio.NewReader(strings.NewReader(wire)), docs, io.Discard, io.Discard, false); err == nil {
		t.Fatal("accepted unknown token")
	}
	if _, err := os.Stat(filepath.Join(dir, "other")); !os.IsNotExist(err) {
		t.Fatalf("unknown file was created: %v", err)
	}
}

func TestPrepareDocuments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "read-only")
	if err := os.WriteFile(path, []byte("original"), 0400); err != nil {
		t.Fatal(err)
	}
	s := settings{Files: []string{path}}
	if _, err := prepareDocuments(s, strings.NewReader(""), false, false, io.Discard); err == nil {
		t.Fatal("opened read-only file without force")
	}
	s.Force = true
	docs, err := prepareDocuments(s, strings.NewReader(""), false, false, io.Discard)
	if err != nil || len(docs) != 1 || string(docs[0].Data) != "original" {
		t.Fatalf("force: %+v, %v", docs, err)
	}
	s.Files = []string{dir}
	if _, err := prepareDocuments(s, strings.NewReader(""), false, false, io.Discard); err == nil {
		t.Fatal("opened directory")
	}
	s.Files = []string{"-", "-"}
	if _, err := prepareDocuments(s, strings.NewReader("stdin"), false, true, io.Discard); err == nil {
		t.Fatal("opened stdin twice")
	}
	s.Files, s.Names = []string{"-"}, []string{"bad\nheader"}
	if _, err := prepareDocuments(s, strings.NewReader("stdin"), false, true, io.Discard); err == nil {
		t.Fatal("accepted header injection")
	}
	docs, err = prepareDocuments(settings{}, strings.NewReader("binary\x00"), false, true, io.Discard)
	if err != nil || len(docs) != 1 || docs[0].Token != "|" || string(docs[0].Data) != "binary\x00" {
		t.Fatalf("implicit stdin: %+v, %v", docs, err)
	}
	docs, err = prepareDocuments(settings{}, strings.NewReader(""), true, true, io.Discard)
	if err != nil || len(docs) != 0 {
		t.Fatalf("terminal with no args: %+v, %v", docs, err)
	}
}

func TestSavePreservesLinksPermissionsAndExistingBackup(t *testing.T) {
	dir := t.TempDir()
	path, alias, link := filepath.Join(dir, "file"), filepath.Join(dir, "hardlink"), filepath.Join(dir, "symlink")
	if err := os.WriteFile(path, []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(link+"~", []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveFile(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{path, alias, link} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "new" {
			t.Fatalf("%s: %q, %v", path, data, err)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("mode changed: %v, %v", info, err)
	}
	data, err := os.ReadFile(link + "~")
	if err != nil || string(data) != "keep" {
		t.Fatalf("existing backup changed: %q, %v", data, err)
	}
	if _, err := os.Stat(link + "~~"); !os.IsNotExist(err) {
		t.Fatalf("recovery copy leaked: %v", err)
	}
}
