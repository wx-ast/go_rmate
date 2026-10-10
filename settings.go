package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var errHelp = errors.New("help requested")
var errVersion = errors.New("version requested")

const usage = `Usage: rmate [OPTION]... FILE...

      --host HOST   Server host (default localhost); auto uses SSH_CONNECTION
  -p, --port PORT   Server port (default 52698)
  -w, --wait        Wait until documents are closed
      --no-wait     Continue handling saves in the background (default)
  -l, --line LINE   Initial line or TextMate selection string
  -m, --name NAME   Display name in the editor
  -t, --type TYPE   File type in the editor
  -f, --force       Open read-only files
  -v, --verbose     Log connection, saves and closes to stderr
  -h, --help        Show help
      --version     Show version

Use - for stdin. Without FILE, read piped stdin; on a terminal, do nothing.
Repeat --line, --name and --type to apply them to successive files.
Options may appear before or after filenames. Use -- before literal filenames.
`

type settings struct {
	Host                       string
	Port                       int
	Wait, Force, Verbose       bool
	Lines, Names, Types, Files []string
}

func loadSettings() (settings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return settings{}, err
	}
	return readSettings([]string{"/etc/rmate.rc", "/usr/local/etc/rmate.rc", filepath.Join(home, ".rmate.rc")}, os.LookupEnv)
}

func readSettings(paths []string, lookup func(string) (string, bool)) (settings, error) {
	s := settings{Host: "localhost", Port: 52698}
	port := "52698"
	for _, path := range paths {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return s, fmt.Errorf("read config %s: %w", path, err)
		}
		scan := bufio.NewScanner(f)
		for scan.Scan() {
			line := strings.TrimSpace(scan.Text())
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				key, value, ok = strings.Cut(line, ":")
			}
			if !ok {
				f.Close()
				return s, fmt.Errorf("invalid config line in %s: %q", path, line)
			}
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "host":
				s.Host = strings.TrimSpace(value)
			case "port":
				port = strings.TrimSpace(value)
			}
		}
		err = scan.Err()
		f.Close()
		if err != nil {
			return s, fmt.Errorf("read config %s: %w", path, err)
		}
	}
	if value, ok := lookup("RMATE_HOST"); ok {
		s.Host = value
	}
	if value, ok := lookup("RMATE_PORT"); ok {
		port = value
	}
	var err error
	s.Port, err = parsePort(port)
	return s, err
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid port %q (expected 1..65535)", value)
	}
	return port, nil
}

func (s *settings) parse(args []string) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			s.Files = append(s.Files, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			s.Files = append(s.Files, arg)
			continue
		}
		if strings.HasPrefix(arg, "--") {
			name, value, hasValue := strings.Cut(arg, "=")
			switch name {
			case "--host", "--port", "--line", "--name", "--type":
				if !hasValue {
					i++
					if i >= len(args) {
						return fmt.Errorf("%s requires a value", name)
					}
					value = args[i]
				}
			default:
				if hasValue {
					return fmt.Errorf("%s does not accept a value", name)
				}
			}
			if err := s.setOption(name, value); err != nil {
				return err
			}
			continue
		}
		for n := 1; n < len(arg); n++ {
			name, value := "-"+string(arg[n]), ""
			if strings.ContainsRune("plmt", rune(arg[n])) {
				if n+1 < len(arg) {
					value = arg[n+1:]
				} else {
					i++
					if i >= len(args) {
						return fmt.Errorf("%s requires a value", name)
					}
					value = args[i]
				}
				n = len(arg)
			}
			if err := s.setOption(name, value); err != nil {
				return err
			}
		}
	}
	if s.Host == "auto" {
		s.Host = "localhost"
		if fields := strings.Fields(os.Getenv("SSH_CONNECTION")); len(fields) > 0 {
			s.Host = fields[0]
		}
	}
	if strings.TrimSpace(s.Host) == "" {
		return fmt.Errorf("host must not be empty")
	}
	return nil
}

func (s *settings) setOption(name, value string) error {
	switch name {
	case "-h", "--help":
		return errHelp
	case "--version":
		return errVersion
	case "--host":
		s.Host = value
	case "-p", "--port":
		port, err := parsePort(value)
		if err != nil {
			return err
		}
		s.Port = port
	case "-w", "--wait":
		s.Wait = true
	case "--no-wait":
		s.Wait = false
	case "-l", "--line":
		s.Lines = append(s.Lines, value)
	case "-m", "--name":
		s.Names = append(s.Names, value)
	case "-t", "--type":
		s.Types = append(s.Types, value)
	case "-f", "--force":
		s.Force = true
	case "-v", "--verbose":
		s.Verbose = true
	default:
		return fmt.Errorf("unknown option %s; use --help", name)
	}
	return nil
}
