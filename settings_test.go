package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCLIOptions(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "192.0.2.10 4444 192.0.2.20 22")
	s := settings{Host: "localhost", Port: 52698}
	err := s.parse([]string{"one.txt", "-wvp1234", "--host=auto", "-l", "3:2", "-mFirst", "-tgo", "two.txt", "--line=8", "--name", "Second", "--no-wait", "--", "-literal"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Host != "192.0.2.10" || s.Port != 1234 || s.Wait || !s.Verbose {
		t.Fatalf("unexpected settings: %+v", s)
	}
	if !reflect.DeepEqual(s.Files, []string{"one.txt", "two.txt", "-literal"}) || !reflect.DeepEqual(s.Lines, []string{"3:2", "8"}) || !reflect.DeepEqual(s.Names, []string{"First", "Second"}) || !reflect.DeepEqual(s.Types, []string{"go"}) {
		t.Fatalf("unexpected lists: %+v", s)
	}
	for _, args := range [][]string{{"-p0"}, {"--port=65536"}, {"--port=no"}, {"--host"}, {"-z"}, {"--wait=yes"}} {
		s := settings{}
		if s.parse(args) == nil {
			t.Errorf("accepted invalid args %v", args)
		}
	}
	s = settings{Host: "auto"}
	t.Setenv("SSH_CONNECTION", "")
	if err := s.parse(nil); err != nil || s.Host != "localhost" {
		t.Fatalf("auto fallback: %+v, %v", s, err)
	}
	s = settings{Host: "localhost"}
	if err := s.parse([]string{"--name", "--help", "file"}); err != nil || s.Names[0] != "--help" {
		t.Fatalf("option value interpreted as help: %v", err)
	}
}

func TestConfigurationPrecedence(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "missing"), filepath.Join(dir, "system"), filepath.Join(dir, "user")}
	for path, content := range map[string]string{paths[1]: "# config\nhost = system\nport = 1111\n", paths[2]: "; user\nhost: user\nport: 2222\n"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"RMATE_HOST": "environment", "RMATE_PORT": "3333"}
	s, err := readSettings(paths, func(key string) (string, bool) { value, ok := env[key]; return value, ok })
	if err != nil || s.Host != "environment" || s.Port != 3333 {
		t.Fatalf("env precedence: %+v, %v", s, err)
	}
	if err := s.parse([]string{"--host=cli", "-p4444"}); err != nil || s.Host != "cli" || s.Port != 4444 {
		t.Fatalf("CLI precedence: %+v, %v", s, err)
	}
	s, err = readSettings(paths, func(string) (string, bool) { return "", false })
	if err != nil || s.Host != "user" || s.Port != 2222 {
		t.Fatalf("file precedence: %+v, %v", s, err)
	}
}
