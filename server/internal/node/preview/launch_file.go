package preview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/lazyre"
	"github.com/nwverse/persona/server/internal/shellcmd"
)

// LaunchFile is where a project keeps its dev servers, under its folder.
const LaunchFile = ".claude/launch.json"

// launchFileMax: a launch.json bigger than this is not one.
const launchFileMax = 256 << 10

// Server is one entry of launch.json as the panel shows it.
type Server struct {
	Name string `json:"name"`
	Port int    `json:"port,omitempty"`
	// URL is the entry's own address (a page of its server), "" for http://localhost:<port>/.
	URL string `json:"url,omitempty"`
	// Command is what Start runs, the program first; empty for an entry that only attaches to a running server.
	Command []string `json:"command,omitempty"`
	// Env are the entry's variables, shown with the command.
	Env map[string]string `json:"env,omitempty"`
	// Cwd is where it runs.
	Cwd       string `json:"cwd,omitempty"`
	Listening bool   `json:"listening"`
	Title     string `json:"title,omitempty"`
	// Problem says why the entry cannot be used (a code); the others are empty then.
	Problem string `json:"problem,omitempty"`
}

// Servers is preview.servers' answer.
type Servers struct {
	// File is the launch.json read, "" when the folder and the ones above it up to the repository's root have none.
	File    string   `json:"file,omitempty"`
	Servers []Server `json:"servers"`
	// Error is why File could not be used (its JSON, its size).
	Error string `json:"error,omitempty"`
	// Browser is the browser previews open in, "" when this computer has none.
	Browser string `json:"browser,omitempty"`
}

// Problems of a launch.json entry.
const (
	problemNoName   = "no_name"
	problemNoPort   = "no_port"   // neither a port nor a local url with one
	problemNotLocal = "not_local" // its url is another computer's: a preview shows this one's
	problemNoRun    = "no_run"    // nothing to run and no url to attach to
	problemBadEnv   = "bad_env"   // an env name that is not one
)

const launchEnvName = `^[A-Za-z_][A-Za-z0-9_]*$`

// FindLaunchFile is the launch.json for a session in dir: dir's own, else the nearest folder above it up to the
// repository's root (or the home folder) that has one.
func (h *Host) FindLaunchFile(dir string) string {
	for i := 0; i < 12 && dir != ""; i++ {
		p := filepath.Join(dir, LaunchFile)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil || dir == h.home {
			return ""
		}
		up := filepath.Dir(dir)
		if up == dir {
			return ""
		}
		dir = up
	}
	return ""
}

// launchEntry is a configuration as written.
type launchEntry struct {
	Name              string            `json:"name"`
	RuntimeExecutable string            `json:"runtimeExecutable"`
	RuntimeArgs       []string          `json:"runtimeArgs"`
	Port              json.Number       `json:"port"`
	URL               string            `json:"url"`
	Cwd               string            `json:"cwd"`
	Env               map[string]string `json:"env"`
}

// ReadLaunchFile reads the entries of a launch.json at path (its folder's parent is the project's root). The entries
// are as written, a Problem on those it cannot use; nothing is probed.
func ReadLaunchFile(path string) ([]Server, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, launchFileMax+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > launchFileMax {
		return nil, fmt.Errorf("%s is over %d KB", LaunchFile, launchFileMax>>10)
	}
	var doc struct {
		Configurations []json.RawMessage `json:"configurations"`
	}
	dec := json.NewDecoder(bytes.NewReader(jsonc(raw)))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %v", LaunchFile, err)
	}
	root := filepath.Dir(filepath.Dir(path))
	out := []Server{}
	for _, c := range doc.Configurations {
		var e launchEntry
		d := json.NewDecoder(bytes.NewReader(c))
		d.UseNumber()
		if err := d.Decode(&e); err != nil {
			s := Server{Name: entryName(c), Problem: problemNoRun}
			if s.Name == "" {
				s.Problem = problemNoName
			}
			out = append(out, s)
			continue
		}
		out = append(out, launchServer(root, e))
	}
	return out, nil
}

// entryName is an entry's name when the rest of it is not what launch.json takes.
func entryName(raw json.RawMessage) string {
	var e struct {
		Name any `json:"name"`
	}
	_ = json.Unmarshal(raw, &e)
	s, _ := e.Name.(string)
	return strings.TrimSpace(s)
}

func launchServer(root string, e launchEntry) Server {
	s := Server{Name: strings.TrimSpace(e.Name)}
	if s.Name == "" {
		s.Problem = problemNoName
		return s
	}
	if p, err := strconv.Atoi(e.Port.String()); err == nil {
		s.Port = p
	}
	if u := strings.TrimSpace(e.URL); u != "" {
		pu, err := url.Parse(u)
		if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || !loopbackHost(pu.Hostname()) {
			s.Problem = problemNotLocal
			return s
		}
		if p, err := strconv.Atoi(pu.Port()); err == nil && s.Port == 0 {
			s.Port = p
		}
		s.URL = pu.String()
	}
	if s.Port <= 0 || s.Port > 65535 {
		s.Problem, s.Port = problemNoPort, 0
		return s
	}
	if exe := strings.TrimSpace(e.RuntimeExecutable); exe != "" {
		for k := range e.Env {
			if valid, _ := regexp.MatchString(launchEnvName, k); !valid {
				s.Problem = problemBadEnv
				return s
			}
		}
		s.Command = append([]string{exe}, e.RuntimeArgs...)
		s.Env = e.Env
		s.Cwd = root
		if c := strings.TrimSpace(e.Cwd); c != "" {
			c = strings.ReplaceAll(c, "${workspaceFolder}", root)
			if !filepath.IsAbs(c) {
				c = filepath.Join(root, c)
			}
			s.Cwd = filepath.Clean(c)
		}
	} else if s.URL == "" {
		s.Problem = problemNoRun
	}
	return s
}

var (
	jsoncComment = lazyre.New(`(?s)"(?:[^"\\]|\\.)*"|//[^\n]*|/\*.*?\*/`)
	jsoncComma   = lazyre.New(`(?s)"(?:[^"\\]|\\.)*"|,(\s*[\]}])`)
)

// jsonc is a JSON-with-comments text (what VS Code writes launch.json as) as JSON: comments and trailing commas out,
// strings untouched.
func jsonc(b []byte) []byte {
	b = jsoncComment.ReplaceAllFunc(b, func(m []byte) []byte {
		if m[0] == '"' {
			return m
		}
		return nil
	})
	return jsoncComma.ReplaceAllFunc(b, func(m []byte) []byte {
		if m[0] == '"' {
			return m
		}
		return m[1:]
	})
}

// loopbackHost: an address of this computer only (localhost, *.localhost, 127.0.0.0/8, ::1).
func loopbackHost(h string) bool {
	h = strings.ToLower(strings.TrimSuffix(strings.Trim(h, "[]"), "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// LaunchCommand is the line typed to run an entry: `env VAR=val …` in front when it has variables (env, so every
// shell takes it), each word quoted.
func LaunchCommand(s *Server) string {
	var words []string
	if len(s.Env) > 0 {
		keys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		words = append(words, "env")
		for _, k := range keys {
			words = append(words, shellcmd.Quote(k+"="+s.Env[k]))
		}
	}
	for _, w := range s.Command {
		words = append(words, shellcmd.Quote(w))
	}
	return strings.Join(words, " ")
}
