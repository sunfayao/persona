package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/lazyre"
	"github.com/sunfayao/persona/server/internal/transcript"
)

const (
	maxConfig    = 64 << 10
	maxDocument  = 64 << 10
	maxDocuments = 12
	maxHits      = 5
)

var (
	ErrDenied      = errors.New("engineering knowledge access denied for this session, space or project")
	errUnavailable = errors.New("engineering knowledge is unavailable")
	errInvalid     = errors.New("use knowledge_search with a query or knowledge_read with a result's path")
	logicalPath    = lazyre.New(`^knowledge/(?:[a-zA-Z0-9_-]+/)*[a-zA-Z0-9_.-]+\.md$`)
	// Remove local references without needing to resolve or disclose any storage location.
	physicalPath = lazyre.New(`(?i)\b(?:file:|[a-z][a-z0-9+.-]*://)[^\s<>"']+|(^|[^\p{L}\p{N}_/-])(?:[a-z]:[/\\]|~[/\\]|\\\\|/|%2f|%5c)[^\s<>"']+`)
)

// Caller uses the machine ID and session attested by the manager's Node tool channel.
type Caller struct{ Machine, Session string }

type Scope struct{ Machine, Space, Project string }

type Request struct {
	Action string `json:"action"`
	Query  string `json:"query,omitempty"`
	Path   string `json:"path,omitempty"`
}

type Grant struct {
	Machine   string   `json:"machine"`
	Space     *string  `json:"space"`
	Project   string   `json:"project"`
	Documents []string `json:"documents"`
}

type Config struct {
	Executable string  `json:"executable,omitempty"`
	Grants     []Grant `json:"grants"`
}

type Dependencies struct {
	Authorize  func(context.Context, Caller) (Scope, bool)
	ConfigFile string
	// Tests inject the same boundaries at which the manager reads policy and invokes the local reader.
	Load func() (Config, error)
	Read func(context.Context, string, string) (string, error)
}

type Domain struct {
	authorize func(context.Context, Caller) (Scope, bool)
	load      func() (Config, error)
	read      func(context.Context, string, string) (string, error)
}

func New(d Dependencies) *Domain {
	if d.Load == nil {
		d.Load = func() (Config, error) { return loadConfig(d.ConfigFile) }
	}
	if d.Read == nil {
		d.Read = readDocument
	}
	return &Domain{authorize: d.Authorize, load: d.Load, read: d.Read}
}

// Run checks the grant and current session before and after reading. Only the granted documents enter scoring.
func (d *Domain) Run(ctx context.Context, caller Caller, req Request) (string, error) {
	query := strings.TrimSpace(req.Query)
	switch req.Action {
	case "knowledge_search":
		if query == "" || len(query) > 240 || req.Path != "" {
			return "", errInvalid
		}
	case "knowledge_read":
		if req.Query != "" || !validPath(req.Path) {
			return "", errInvalid
		}
	default:
		return "", errInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	scope, ok := d.authorize(ctx, caller)
	if !ok || scope.Project == "" || scope.Machine == "" {
		return "", ErrDenied
	}
	cfg, docs, err := d.allowed(scope)
	if err != nil {
		return "", err
	}
	var result any
	if req.Action == "knowledge_read" {
		if !slices.Contains(docs, req.Path) {
			return "", ErrDenied
		}
		content, err := d.clean(ctx, cfg.Executable, req.Path)
		if err != nil {
			return "", err
		}
		result = struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}{req.Path, clip(content, 12<<10)}
	} else {
		hits, err := d.search(ctx, cfg.Executable, docs, query)
		if err != nil {
			return "", err
		}
		result = struct {
			Hits []hit `json:"hits"`
		}{hits}
	}
	current, ok := d.authorize(ctx, caller)
	if !ok || current != scope {
		return "", ErrDenied
	}
	currentCfg, currentDocs, err := d.allowed(scope)
	if err != nil || currentCfg.Executable != cfg.Executable || !slices.Equal(currentDocs, docs) {
		return "", ErrDenied
	}
	raw, err := json.Marshal(result)
	return string(raw), err
}

func (d *Domain) allowed(scope Scope) (Config, []string, error) {
	cfg, err := d.load()
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil, ErrDenied
	}
	if err != nil || cfg.Executable != "" && !filepath.IsAbs(cfg.Executable) {
		return Config{}, nil, errUnavailable
	}
	var docs []string
	for _, grant := range cfg.Grants {
		if grant.Machine == scope.Machine && grant.Project == scope.Project && grant.Space != nil && *grant.Space == scope.Space {
			docs = append(docs, grant.Documents...)
		}
	}
	slices.Sort(docs)
	docs = slices.Compact(docs)
	if len(docs) == 0 {
		return Config{}, nil, ErrDenied
	}
	if len(docs) > maxDocuments {
		return Config{}, nil, errUnavailable
	}
	for _, doc := range docs {
		if !validPath(doc) {
			return Config{}, nil, errUnavailable
		}
	}
	return cfg, docs, nil
}

func validPath(doc string) bool {
	return len(doc) <= 256 && path.Clean(doc) == doc && logicalPath.MatchString(doc)
}

type hit struct {
	Path    string `json:"path"`
	Snippet string `json:"snippet"`
	score   int
}

func (d *Domain) search(ctx context.Context, executable string, docs []string, query string) ([]hit, error) {
	terms := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '-'
	})
	slices.Sort(terms)
	terms = slices.Compact(terms)
	if len(terms) == 0 {
		return nil, errInvalid
	}
	hits := []hit{}
	for _, doc := range docs {
		content, err := d.clean(ctx, executable, doc)
		if err != nil {
			return nil, err
		}
		best := hit{Path: doc}
		for _, line := range strings.Split(content, "\n") {
			lower := strings.ToLower(line)
			score := 0
			for _, term := range terms {
				if strings.Contains(lower, term) {
					score++
				}
			}
			if score > best.score {
				best.score, best.Snippet = score, clip(strings.TrimSpace(line), 400)
			}
		}
		if best.score > 0 {
			hits = append(hits, best)
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return b.score - a.score })
	return hits[:min(maxHits, len(hits))], nil
}

func (d *Domain) clean(ctx context.Context, executable, doc string) (string, error) {
	raw, err := d.read(ctx, executable, doc)
	if err != nil || len(raw) > maxDocument {
		return "", errUnavailable
	}
	text, err := transcript.SanitizeShareText(raw)
	if err != nil {
		return "", errUnavailable
	}
	return physicalPath.ReplaceAllStringFunc(text, func(value string) string {
		first, n := utf8.DecodeRuneInString(value)
		if !unicode.IsLetter(first) && first != '/' && first != '\\' && first != '~' && first != '%' {
			return value[:n] + "[local reference removed]"
		}
		return "[local reference removed]"
	}), nil
}

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max] + "…"
}

func loadConfig(file string) (Config, error) {
	if file == "" {
		return Config{}, os.ErrNotExist
	}
	f, err := os.Open(file)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxConfig+1))
	if err != nil || len(raw) > maxConfig {
		return Config{}, errUnavailable
	}
	var cfg Config
	err = json.Unmarshal(raw, &cfg)
	return cfg, err
}

func readDocument(ctx context.Context, executable, doc string) (string, error) {
	if executable == "" {
		executable = "knowledge"
	}
	cmd := exec.CommandContext(ctx, executable, "read", "--body", doc)
	var out limitedBuffer
	cmd.Stdout = &out
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > maxDocument-b.Len() {
		return 0, errUnavailable
	}
	return b.Buffer.Write(p)
}
