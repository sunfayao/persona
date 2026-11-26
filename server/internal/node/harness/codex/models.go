package codex

import (
	"bufio"
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

type codexCachedModel struct {
	Slug              string              `json:"slug"`
	Model             string              `json:"model"`
	ID                string              `json:"id"`
	DisplayName       string              `json:"display_name"`
	DisplayNameCamel  string              `json:"displayName"`
	Description       string              `json:"description"`
	Visibility        string              `json:"visibility"`
	Hidden            bool                `json:"hidden"`
	DefaultLevel      string              `json:"default_reasoning_level"`
	DefaultLevelCamel string              `json:"defaultReasoningEffort"`
	ServiceTiers      []proto.ServiceTier `json:"service_tiers"`
	ServiceTiersCamel []proto.ServiceTier `json:"serviceTiers"`
	Levels            []struct {
		Effort string `json:"effort"`
	} `json:"supported_reasoning_levels"`
	LevelsCamel []struct {
		Effort string `json:"reasoningEffort"`
	} `json:"supportedReasoningEfforts"`
}

func (m codexCachedModel) modelID() string {
	return strings.TrimSpace(cmp.Or(m.Slug, m.Model, m.ID))
}

func (m codexCachedModel) visible() bool {
	if m.Hidden {
		return false
	}
	return m.Visibility == "" || m.Visibility == "list"
}

func (m codexCachedModel) efforts() (levels []string, def string, tiers []proto.ServiceTier) {
	levels = []string{}
	for _, x := range m.Levels {
		if e := strings.TrimSpace(x.Effort); e != "" && !slices.Contains(levels, e) {
			levels = append(levels, e)
		}
	}
	for _, x := range m.LevelsCamel {
		if e := strings.TrimSpace(x.Effort); e != "" && !slices.Contains(levels, e) {
			levels = append(levels, e)
		}
	}
	def = strings.TrimSpace(cmp.Or(m.DefaultLevel, m.DefaultLevelCamel))
	if len(levels) == 0 && def == "none" {
		def = ""
	}
	tiers = m.ServiceTiers
	if len(tiers) == 0 {
		tiers = m.ServiceTiersCamel
	}
	return levels, def, tiers
}

func readCodexCacheEntries(home string) ([]codexCachedModel, bool) {
	if home == "" {
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(home, "models_cache.json"))
	if err != nil {
		return nil, false
	}
	var doc struct {
		Models []codexCachedModel `json:"models"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return nil, false
	}
	return doc.Models, true
}

// readCodexConfigDefault reads top-level `model` and `model_reasoning_effort` from <home>/config.toml without touching it.
func readCodexConfigDefault(home string) (model, effort string) {
	if home == "" {
		return "", ""
	}
	f, err := os.Open(filepath.Join(home, "config.toml"))
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			break // only top-level keys before the first table header
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if idx := strings.IndexByte(v, '#'); idx >= 0 {
			// Strip trailing comment outside quotes when simple.
			if strings.Count(v[:idx], `"`)%2 == 0 && strings.Count(v[:idx], `'`)%2 == 0 {
				v = v[:idx]
			}
		}
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		switch k {
		case "model":
			if proto.ValidModelID(v) {
				model = v
			}
		case "model_reasoning_effort":
			effort = v
		}
	}
	return model, effort
}

// readCodexHomeModels reads visible models from <home>/models_cache.json and <home>/config.toml as a local fallback when
// `codex app-server` model/list is unavailable or empty. Full provider-prefixed slugs (e.g. acme/gpt-5-codex) are kept.
func ReadHomeModels(home string) []proto.AgentModel {
	entries, hasCache := readCodexCacheEntries(home)
	cfgModel, cfgEffort := readCodexConfigDefault(home)
	if !hasCache && cfgModel == "" {
		return []proto.AgentModel{}
	}
	out := make([]proto.AgentModel, 0, len(entries))
	seen := map[string]bool{}
	hasDefault := false
	for _, m := range entries {
		id := m.modelID()
		if !m.visible() || id == "" || !proto.ValidModelID(id) || seen[id] {
			continue
		}
		seen[id] = true
		levels, def, tiers := m.efforts()
		isDef := cfgModel != "" && id == cfgModel
		if isDef {
			hasDefault = true
		}
		name := strings.TrimSpace(cmp.Or(m.DisplayName, m.DisplayNameCamel, id))
		out = append(out, proto.AgentModel{
			ID:            id,
			Name:          name,
			Description:   strings.TrimSpace(m.Description),
			Default:       isDef,
			Efforts:       levels,
			DefaultEffort: def,
			ServiceTiers:  tiers,
		})
	}
	if cfgModel != "" && !seen[cfgModel] {
		def := cfgEffort
		if def == "" {
			def = "medium"
		}
		out = slices.Insert(out, 0, proto.AgentModel{
			ID:            cfgModel,
			Name:          cfgModel,
			Default:       true,
			Efforts:       slices.Clone(proto.CodexEffortLevels),
			DefaultEffort: def,
		})
		hasDefault = true
	}
	if !hasDefault && len(out) > 0 {
		out[0].Default = true
	}
	return out
}

// readCodexHomeModelEfforts reads effort metadata (including hidden models) from <home>/models_cache.json.
func ReadHomeModelEfforts(home string) (map[string]ModelEfforts, bool) {
	entries, ok := readCodexCacheEntries(home)
	if !ok || len(entries) == 0 {
		return nil, false
	}
	models := make(map[string]ModelEfforts, len(entries))
	for _, m := range entries {
		levels, def, tiers := m.efforts()
		for _, k := range []string{strings.TrimSpace(m.Slug), strings.TrimSpace(m.Model), strings.TrimSpace(m.ID)} {
			if k != "" {
				models[k] = ModelEfforts{Levels: levels, Default: def, Tiers: tiers}
			}
		}
	}
	return models, len(models) > 0
}

// reconcileCodexEffort picks a valid effort on meta when a model switch leaves the previous effort incompatible.
func ReconcileEffort(meta ModelEfforts) string {
	if len(meta.Levels) == 0 {
		return ""
	}
	if meta.Default != "" && slices.Contains(meta.Levels, meta.Default) {
		return meta.Default
	}
	if slices.Contains(meta.Levels, "medium") {
		return "medium"
	}
	return meta.Levels[0]
}
