package codex

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// A managed Codex provider's gateway may serve Codex's own model catalog: each model's context window, compaction
// limit, thinking levels and instructions, in the shape Codex reads from its backend (codex-rs ModelInfo). Without it
// Codex treats the gateway as OpenAI: its picker lists the bundled OpenAI models and a model only the gateway has runs
// on fallback metadata. Codex reads a catalog from a provider's model_catalog_url (0.156 and later; by default since
// 0.161, when api_key_model_discovery became stable), asking `<url>?client_version=<its version>` with the provider's
// key. Once a provider has one, it is the only list Codex uses: one that fails leaves Codex with no list, so the
// URL is passed only where the node found a catalog there, and kept through a probe that could not tell.
//
// The node looks for it by the answer's shape, never the gateway's name ("Gateway model
// catalog"): the probe asks what Codex would, and a catalog is an object whose `models` are Codex model entries.

// codexCatalogFields are the fields every Codex model entry has, with the JSON kind of each ('"' a string, 'b' a
// boolean, 'i' a whole number, '[' a list, '{' an object): those codex-rs ModelInfo cannot do without in every version that
// reads a catalog by default (0.161 to 0.169 alike), slug aside. One without them fails Codex's whole catalog: it then
// lists nothing and runs every model on fallback metadata, worse than no catalog.
func codexCatalogFields() map[string]byte {
	return map[string]byte{"display_name": '"', "supported_reasoning_levels": '[', "shell_type": '"',
		"visibility": '"', "supported_in_api": 'b', "priority": 'i', "support_verbosity": 'b', "truncation_policy": '{',
		"experimental_supported_tools": '['}
}

// jsonKind is raw's kind as codexCatalogFields writes it; 0 for null, nothing or anything else.
func jsonKind(raw json.RawMessage) byte {
	raw = bytes.TrimSpace(raw)
	switch {
	case len(raw) == 0:
		return 0
	case raw[0] == '"', raw[0] == '[', raw[0] == '{':
		return raw[0]
	case raw[0] == 't', raw[0] == 'f':
		return 'b'
	case raw[0] == '-', raw[0] >= '0' && raw[0] <= '9':
		if bytes.ContainsAny(raw, ".eE") { // Codex's numbers here are integers: 1.0 or 1e3 fails its decoding
			return 0
		}
		return 'i'
	}
	return 0
}

// fieldsOfKinds is obj's fields when it is a JSON object with each of fields of its kind; nil otherwise.
func fieldsOfKinds(obj json.RawMessage, fields map[string]byte) map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if jsonKind(obj) != '{' || json.Unmarshal(obj, &m) != nil {
		return nil
	}
	for name, kind := range fields {
		if jsonKind(m[name]) != kind {
			return nil
		}
	}
	return m
}

// isCodexCatalog: body is a catalog Codex can read, `{"models":[…]}` with at least one entry, each with a unique,
// non-empty slug and every field of codexCatalogFields (each thinking level an effort and a description, the truncation
// policy a mode and a limit), and one at least that its picker lists. OpenAI's model list ({"data":[…]}) is none. The
// words inside (a level, a shell type, a visibility) are not checked: they change with Codex, whose version the gateway
// was told.
func IsModelCatalog(body []byte) bool {
	var doc struct {
		Models []json.RawMessage `json:"models"`
	}
	if json.Unmarshal(body, &doc) != nil || len(doc.Models) == 0 {
		return false
	}
	seen, listed := map[string]bool{}, false
	fields := codexCatalogFields()
	for _, raw := range doc.Models {
		m := fieldsOfKinds(raw, fields)
		var slug, visibility string
		var levels []json.RawMessage
		if m == nil || jsonKind(m["slug"]) != '"' || json.Unmarshal(m["slug"], &slug) != nil || strings.TrimSpace(slug) == "" ||
			seen[slug] || json.Unmarshal(m["visibility"], &visibility) != nil || json.Unmarshal(m["supported_reasoning_levels"], &levels) != nil ||
			fieldsOfKinds(m["truncation_policy"], map[string]byte{"mode": '"', "limit": 'i'}) == nil {
			return false
		}
		for _, level := range levels {
			if fieldsOfKinds(level, map[string]byte{"effort": '"', "description": '"'}) == nil {
				return false
			}
		}
		seen[slug] = true
		listed = listed || visibility == "list"
	}
	return listed
}

// parseCodexVersion reads `codex --version`'s "codex-cli 0.167.0": its first word that is a version, "0.167.0", or
// "0.168.0-" for a prerelease (0.168.0-alpha.2); "" when there is none.
func ParseVersion(out string) string {
	for _, word := range strings.Fields(out) {
		v, _, pre := strings.Cut(strings.SplitN(word, "+", 2)[0], "-")
		parts := strings.Split(v, ".")
		if len(parts) != 3 || slices.ContainsFunc(parts, func(p string) bool { _, err := strconv.Atoi(p); return err != nil }) {
			continue
		}
		if pre {
			return v + "-"
		}
		return v
	}
	return ""
}

// codexReadsCatalog: a codex of version (parseCodexVersion's) reads a provider's catalog by default; a prerelease of
// the first one that does is taken for one that does not yet.
func ReadsCatalog(version string) bool {
	if version == "" {
		return false
	}
	v, pre := strings.CutSuffix(version, "-")
	var got [3]int
	for i, p := range strings.Split(v, ".") {
		got[i], _ = strconv.Atoi(p)
	}
	for i, min := range [3]int{0, 161, 0} {
		if got[i] != min {
			return got[i] > min
		}
	}
	return !pre
}

// codexCatalogClientVersion is the client_version Codex sends for version: major.minor.patch, no prerelease.
func CatalogClientVersion(version string) string { return strings.TrimSuffix(version, "-") }
