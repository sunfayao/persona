package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/lazyre"
)

// Personas are look packages installed in the hub's personas directory or supplied by an extension.
const (
	personaFormat = 1
	// A package is a few hundred KB of PNGs; the bounds keep a mistake (a 4K source image left in) off the phones.
	personaMaxFile  = 4 << 20
	personaMaxTotal = 16 << 20
)

var personaIDPattern = lazyre.New(`^[a-z0-9][a-z0-9-]{0,31}$`)

// personaStates are the dot's states; idle is the one a package must have (the app shows it for any it lacks).
var personaStates = []string{"idle", "thinking", "working", "needs_you", "offline"}

// personaManifest is what the hub checks in persona.json; the app reads the rest (colors, frame rates, about).
type personaManifest struct {
	Format int    `json:"format"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar struct {
		Sizes map[string]string `json:"sizes"`
	} `json:"avatar"`
	Dot struct {
		States map[string]struct {
			File string `json:"file"`
		} `json:"states"`
	} `json:"dot"`
}

// personaInfo is one entry of GET /api/v1/personas: the manifest as written, and a rev that changes with any byte of
// what the phone downloads (the phone keeps a copy per rev).
type personaInfo struct {
	ID       string          `json:"id"`
	Rev      string          `json:"rev"`
	Bytes    int64           `json:"bytes"`
	Manifest json.RawMessage `json:"manifest"`
}

// personaPackage is a package that checks out: where its files are and what they are.
type personaPackage struct {
	id    string
	fsys  fs.FS
	files map[string]int64 // every regular file (slash paths, dot files left out) → its size
	total int64
	raw   []byte // persona.json as written
	// source is the extension it comes from (looks.go; nil: the hub's own).
	source *lookSource
}

func (h *Hub) personasDir() string { return filepath.Join(h.cfg.dir, "personas") }

// personaPackages lists the hub's installed looks followed by the extensions' looks.
func (h *Hub) personaPackages() []*personaPackage {
	out := []*personaPackage{}
	if entries, err := os.ReadDir(h.personasDir()); err == nil {
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			p, err := openPersona(os.DirFS(filepath.Join(h.personasDir(), e.Name())), e.Name())
			if err != nil {
				h.logf("persona %s left out: %v", e.Name(), err)
				continue
			}
			out = append(out, p)
		}
	}
	// Then the extensions' looks (looks.go): a package of the hub's of the same id keeps its place.
	for _, p := range h.extensionLooks() {
		if !slices.ContainsFunc(out, func(q *personaPackage) bool { return q.id == p.id }) {
			out = append(out, p)
		}
	}
	return out
}

// personaPackage is the package of that id: its directory when that checks out, else an extension look.
func (h *Hub) personaPackage(id string) (*personaPackage, error) {
	if !personaIDPattern.MatchString(id) {
		return nil, fmt.Errorf("no persona %q", id)
	}
	p, dirErr := openPersona(os.DirFS(filepath.Join(h.personasDir(), id)), id)
	if dirErr == nil {
		return p, nil
	}
	for _, p := range h.extensionLooks() {
		if p.id == id {
			return p, nil
		}
	}
	return nil, dirErr
}

// personas lists the packages that check out, with their manifest and revision.
func (h *Hub) personas() []personaInfo {
	out := []personaInfo{}
	for _, p := range h.personaPackages() {
		info, err := h.personaInfo(p)
		if err != nil {
			h.logf("persona %s left out: %v", p.id, err)
			continue
		}
		out = append(out, info)
	}
	return out
}

// personaInfo is the package's entry in the list, including a revision over its files.
func (h *Hub) personaInfo(p *personaPackage) (personaInfo, error) {
	names := make([]string, 0, len(p.files))
	for f := range p.files {
		names = append(names, f)
	}
	rev, err := personaRev(p.fsys, names)
	if err != nil {
		return personaInfo{}, err
	}
	return personaInfo{ID: p.id, Rev: rev, Bytes: p.total, Manifest: p.raw}, nil
}

// personaRev sums the package files into a rev.
func personaRev(fsys fs.FS, names []string) (string, error) {
	sort.Strings(names)
	sum := sha256.New()
	for _, f := range names {
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(sum, "%s\x00%d\x00", f, len(b))
		sum.Write(b)
	}
	return hex.EncodeToString(sum.Sum(nil))[:16], nil
}

func manifestFiles(m personaManifest) []string {
	seen := map[string]bool{}
	var out []string
	add := func(f string) {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for _, f := range m.Avatar.Sizes {
		add(f)
	}
	for _, st := range m.Dot.States {
		add(st.File)
	}
	return out
}

// openPersona checks the package in fsys (its root is the package's directory): persona.json, the files it names and
// the size; for a tiered one, that the tiers are there and their manifests name files the package has.
func openPersona(fsys fs.FS, id string) (*personaPackage, error) {
	if !personaIDPattern.MatchString(id) {
		return nil, errors.New("the directory's name is not an id (a-z, 0-9, -)")
	}
	if extension.ReservedLookID(id) {
		return nil, fmt.Errorf("%q is a reserved look id", id)
	}
	files, total, err := personaFiles(fsys)
	if err != nil {
		return nil, err
	}
	if total > personaMaxTotal {
		return nil, fmt.Errorf("%d bytes, more than %d", total, personaMaxTotal)
	}
	p := &personaPackage{id: id, fsys: fsys, files: files, total: total}
	if _, ok := files["persona.json"]; !ok {
		return nil, errors.New("no persona.json")
	}
	if p.raw, err = fs.ReadFile(fsys, "persona.json"); err != nil {
		return nil, err
	}
	if _, err = p.checkManifest(p.raw); err != nil {
		return nil, fmt.Errorf("persona.json: %w", err)
	}
	return p, nil
}

// checkManifest parses a persona.json of this package and checks what the hub holds every manifest to.
func (p *personaPackage) checkManifest(raw []byte) (personaManifest, error) {
	var m personaManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	switch {
	case m.Format != personaFormat:
		return m, fmt.Errorf("format %d, this hub reads %d", m.Format, personaFormat)
	case m.ID != p.id:
		return m, fmt.Errorf("id %q is not the directory's name", m.ID)
	case strings.TrimSpace(m.Name) == "":
		return m, errors.New("no name")
	case len(m.Avatar.Sizes) == 0:
		return m, errors.New("no avatar")
	}
	if _, ok := m.Dot.States["idle"]; !ok {
		return m, errors.New("no idle dot")
	}
	for s := range m.Dot.States {
		if !isPersonaState(s) {
			return m, fmt.Errorf("unknown dot state %q", s)
		}
	}
	for _, f := range manifestFiles(m) {
		if _, err := p.file(f); err != nil {
			return m, err
		}
	}
	return m, nil
}

func isPersonaState(s string) bool {
	for _, x := range personaStates {
		if s == x {
			return true
		}
	}
	return false
}

// file is a file of the package by its name in a manifest or a request: a relative path inside the package, no dot
// files, a regular file (not a link out of it) within the size bound. It returns the name to open.
func (p *personaPackage) file(name string) (string, error) {
	clean := path.Clean(name)
	if name == "" || path.IsAbs(name) || clean != name || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("file %q: not a plain path inside the package", name)
	}
	for _, part := range strings.Split(clean, "/") {
		if strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("file %q: hidden", name)
		}
	}
	size, ok := p.files[clean]
	if !ok {
		return "", fmt.Errorf("file %q: not a regular file of the package", name)
	}
	if size > personaMaxFile {
		return "", fmt.Errorf("file %q: %d bytes, more than %d", name, size, personaMaxFile)
	}
	return clean, nil
}

// personaFiles is every regular file in the package (slash paths), dot files and links left out, and their total size.
func personaFiles(fsys fs.FS) (map[string]int64, int64, error) {
	files := map[string]int64{}
	var total int64
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files[p] = info.Size()
		total += info.Size()
		return nil
	})
	return files, total, err
}

func (h *Hub) personaRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/personas", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"personas": h.personas()})
	})
	// A file of a package that checks out (any of its files: a manifest's, or a preview next to them).
	api.HandleFunc("GET /api/v1/personas/{id}/{file...}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		p, err := h.personaPackage(id)
		if err != nil {
			writeErr(w, http.StatusNotFound, fmt.Errorf("no persona %q", id))
			return
		}
		name, err := p.file(r.PathValue("file"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		f, err := p.fsys.Open(name)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		defer f.Close()
		ct := mime.TypeByExtension(path.Ext(name))
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		// At the rev the apps were told (?rev=), a file never changes: the browser may keep it.
		immutable := false
		if rev := r.URL.Query().Get("rev"); rev != "" {
			info, err := h.personaInfo(p)
			immutable = err == nil && info.Rev == rev
		}
		personaFileHeaders(w, name, immutable)
		_, _ = io.Copy(w, f)
	})
}
