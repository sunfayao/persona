package extmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/volc"
)

// An extension's settings in persona_settings (#2385): extension.<id> for every extension the user can configure that has
// a form, read and changed in a conversation the way Settings › Extensions does it. get lists the extension's own switch
// (enabled) and each field (its title, kind, choices and value); set takes only the fields to change, and enabled, is
// held to the schema before the card, shows each before → after on the phone, and once approved saves through the
// registry's SetConfig (the extension restarts with it) and switches it as the Settings page does (setByUser). Secrets
// never pass here; an extension set in manager.json is not listed (it is changed there).

const extensionSettingPrefix = "extension."

// extensionSettings is one entry per configurable extension with a form.
func (h *Manager) extensionSettings() []selfSetting {
	if h.extensions == nil || h.extensions.reg == nil {
		return nil
	}
	var out []selfSetting
	for _, p := range h.extensions.reg.Snapshots() { // its switch is read here, and SetEnabled writes it
		if p.ReadOnly() || p.Record.Source == extension.SourceDev || p.Schema == nil || len(assistantExtensionFields(p.Schema.Fields)) == 0 {
			continue
		}
		id, name := p.Record.ID, extensionDisplayName(p)
		title := i18n.T(h.userLang(), "hub-self_settings_extensions.title", i18n.Args{"name": name})
		out = append(out, selfSetting{key: extensionSettingPrefix + id, title: title,
			about: fmt.Sprintf("the extension %s (%s), as in Settings › Extensions › %s: its own switch (enabled) and its settings; "+
				"get lists every field with its title, kind, choices and current value", name, id, name),
			value: `an object of only the fields to change, {"<field>": <value>, …} (an object field takes an object of its own ` +
				`fields to change; "enabled": true or false switches the extension itself on or off); null puts a field back to ` +
				`its default. Secrets are not set here`,
			now: "enabled: " + strconv.FormatBool(p.Record.Enabled),
			get: func(context.Context) (string, error) { return h.extensionSettingsText(id) },
			set: func(_ context.Context, v json.RawMessage) (*selfChange, error) {
				return h.setExtensionSettings(id, title, v)
			}})
	}
	return out
}

func extensionDisplayName(p *extension.Extension) string {
	if p.Manifest != nil && strings.TrimSpace(p.Manifest.Name) != "" {
		return p.Manifest.Name
	}
	return p.Record.ID
}

// savedSettings is p's configuration as saved (no defaults), by field.
func (h *Manager) savedSettings(p *extension.Extension) map[string]json.RawMessage {
	saved := map[string]json.RawMessage{}
	_ = json.Unmarshal(h.extensions.reg.SavedConfig(p), &saved)
	return saved
}

// extensionSettingsText is get's answer: every field, in the form's order, for the model.
func (h *Manager) extensionSettingsText(id string) (string, error) {
	p := h.extensions.reg.Snapshot(id)
	if p == nil {
		return "", fmt.Errorf("no extension %s", id)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Settings of %s (%s); set changes only the fields given, null puts one back to its default:\n", extensionDisplayName(p), p.Record.ID)
	fmt.Fprintf(&b, "- enabled: true or false; now %t (the extension itself: while it is false it runs nothing and offers no "+
		"tools, whatever the fields below say)\n", p.Record.Enabled)
	saved := h.savedSettings(p)
	for _, f := range assistantExtensionFields(p.Schema.Fields) {
		if isVolc(p) && f.Name == volcMeetingsField {
			continue // the owner's choice of meeting transcription, never set here
		}
		if f.Type == "object" {
			fmt.Fprintf(&b, "- %s%s: a group of these fields (set takes {\"%s\": {<field>: <value>}})\n", f.Name, quotedTitle(f), f.Name)
			if f.Description != "" {
				fmt.Fprintf(&b, "  %s\n", clip(oneLine(f.Description), 300))
			}
			group := map[string]json.RawMessage{}
			_ = json.Unmarshal(saved[f.Name], &group)
			for _, sub := range f.Fields {
				writeFieldLine(&b, "  ", sub, group)
			}
			continue
		}
		writeFieldLine(&b, "", f, saved)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func assistantExtensionFields(fields []extension.Field) []extension.Field {
	return slices.DeleteFunc(slices.Clone(fields), func(f extension.Field) bool { return f.EntryList() })
}

func quotedTitle(f extension.Field) string {
	if f.Title == "" {
		return ""
	}
	return " (" + strconv.Quote(f.Title) + ")"
}

// writeFieldLine is one field for the model: name, title, kind, value (or its default), then what it is for.
func writeFieldLine(b *strings.Builder, indent string, f extension.Field, saved map[string]json.RawMessage) {
	now := "not set"
	if v, ok := saved[f.Name]; ok && !isJSONNull(v) {
		now = string(v)
	} else if f.Default != nil {
		now = string(f.Default) + " (the default)"
	}
	fmt.Fprintf(b, "%s- %s%s: %s; now %s\n", indent, f.Name, quotedTitle(f), fieldKind(f), now)
	if f.Description != "" {
		fmt.Fprintf(b, "%s  %s\n", indent, clip(oneLine(f.Description), 300))
	}
}

// fieldKind says what a field takes, in the model's terms.
func fieldKind(f extension.Field) string {
	if len(f.Enum) > 0 {
		choices := make([]string, 0, len(f.Enum))
		for _, c := range f.Enum {
			s := string(c.Value)
			if c.Title != "" {
				s += " (" + c.Title + ")"
			}
			choices = append(choices, s)
		}
		return "one of " + strings.Join(choices, ", ")
	}
	switch f.Type {
	case "boolean":
		return "true or false"
	case "integer", "number":
		kind := map[string]string{"integer": "a whole number", "number": "a number"}[f.Type]
		if f.Minimum != nil {
			kind += " from " + strconv.FormatFloat(*f.Minimum, 'f', -1, 64)
		}
		if f.Maximum != nil {
			kind += " up to " + strconv.FormatFloat(*f.Maximum, 'f', -1, 64)
		}
		return kind
	case "array":
		if f.Items == nil {
			return "a list"
		}
		return "a list, each " + fieldKind(*f.Items)
	}
	switch f.Format {
	case "machine":
		kind := "a machine id (machines_list)"
		if len(f.OS) > 0 {
			kind += " of " + strings.Join(f.OS, "/")
		}
		return kind
	case "launcher":
		return "a launcher name of this hub"
	case "uri", "email", "hostname":
		return "text, " + map[string]string{"uri": "a URL", "email": "an email address", "hostname": "a host name"}[f.Format]
	}
	if f.MaxLength != nil {
		return "text of at most " + strconv.Itoa(*f.MaxLength) + " characters"
	}
	return "text"
}

func isJSONNull(v json.RawMessage) bool { return string(bytes.TrimSpace(v)) == "null" }

// patchSettings applies the fields of patch to saved (null deletes one, an object field takes its own fields the same
// way) and says which are not the form's.
func patchSettings(fields []extension.Field, saved, patch map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	next := maps.Clone(saved)
	if next == nil {
		next = map[string]json.RawMessage{}
	}
	for k, v := range patch {
		f := fieldNamed(fields, k)
		if f == nil {
			return nil, fmt.Errorf("there is no setting %q (get lists them)", k)
		}
		if isJSONNull(v) {
			delete(next, k)
			continue
		}
		if f.Type != "object" {
			next[k] = v
			continue
		}
		var sub map[string]json.RawMessage
		if json.Unmarshal(v, &sub) != nil || sub == nil {
			return nil, fmt.Errorf("%s: an object of its fields to change", k)
		}
		group := map[string]json.RawMessage{}
		if old, ok := next[k]; ok {
			_ = json.Unmarshal(old, &group)
		}
		for sk, sv := range sub {
			if fieldNamed(f.Fields, sk) == nil {
				return nil, fmt.Errorf("%s has no setting %q (get lists them)", k, sk)
			}
			if isJSONNull(sv) {
				delete(group, sk)
			} else {
				group[sk] = sv
			}
		}
		if len(group) == 0 {
			delete(next, k)
		} else {
			next[k], _ = json.Marshal(group)
		}
	}
	return next, nil
}

func fieldNamed(fields []extension.Field, name string) *extension.Field {
	for i := range fields {
		if fields[i].Name == name {
			return &fields[i]
		}
	}
	return nil
}

// sameValue says whether two values (either maybe missing) are the same JSON.
func sameValue(a, b json.RawMessage) bool {
	var x, y any
	if len(a) > 0 && json.Unmarshal(a, &x) != nil {
		return false
	}
	if len(b) > 0 && json.Unmarshal(b, &y) != nil {
		return false
	}
	ab, _ := json.Marshal(x)
	bb, _ := json.Marshal(y)
	return bytes.Equal(ab, bb)
}

// setExtensionSettings is set's card: the switch and the fields that change, before → after, checked first.
func (h *Manager) setExtensionSettings(id, title string, v json.RawMessage) (*selfChange, error) {
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(v, &patch); err != nil || patch == nil {
		return nil, errors.New(`value must be an object of the fields to change, like {"<field>": <value>} or {"enabled": true}`)
	}
	p := h.extensions.reg.Snapshot(id)
	if p == nil {
		return nil, fmt.Errorf("no extension %s", id)
	}
	if _, ok := patch[volcMeetingsField]; ok && id == volc.ExtensionID {
		return nil, errors.New(volcMeetingsField + " is the owner's choice of meeting transcription: only Settings › Extensions or Recording settings on a device change it")
	}
	if p.ReadOnly() {
		return nil, fmt.Errorf("%s is set in manager.json: change it there (or in the admin page)", id)
	}
	name, wasOn := extensionDisplayName(p), p.Record.Enabled
	on, asked := wasOn, false // enabled, the extension's own switch, is none of its fields
	if raw, ok := patch["enabled"]; ok {
		var b any
		_ = json.Unmarshal(raw, &b)
		if on, asked = b.(bool); !asked {
			return nil, errors.New("enabled takes true or false (the extension's own switch)")
		}
		delete(patch, "enabled")
	}
	switches, word := on != wasOn, map[bool]string{true: "on", false: "off"}[on]
	if switches && p.Full.IsPack() {
		return nil, fmt.Errorf("%s is an extension pack: switch it in Settings › Extensions, which switches its members with it", name)
	}
	if switches && !on { // what the Settings page asks about before it switches one off
		if deps := h.extensions.dependentsOf([]string{id}); len(deps) > 0 {
			names := make([]string, 0, len(deps))
			for _, d := range deps {
				names = append(names, d.Name)
			}
			return nil, fmt.Errorf("%s is needed by %s, which are on: switch those off first", name, strings.Join(names, ", "))
		}
	}
	before := h.savedSettings(p)
	fields := assistantExtensionFields(p.Schema.Fields)
	next, err := patchSettings(fields, before, patch)
	if err != nil {
		return nil, err
	}
	if len(patch) > 0 { // enabled alone switches it whatever its settings are, as the Settings page does
		raw, _ := json.Marshal(next)
		if err := h.checkExtensionSettings(p, raw); err != nil {
			return nil, err
		}
	}
	l := h.userLang()
	lines := h.settingChanges(l, fields, before, next, "")
	configures := len(lines) > 0
	if switches {
		toggle := extension.Field{Type: "boolean"}
		lines = append([]string{changeSummary(l, name, h.fieldValueText(l, toggle, json.RawMessage(strconv.FormatBool(wasOn))),
			h.fieldValueText(l, toggle, json.RawMessage(strconv.FormatBool(on))))}, lines...)
	}
	switch {
	case len(lines) == 0 && asked:
		return &selfChange{summary: fmt.Sprintf("%s is already switched %s (enabled: %t): nothing to change.", name, word, on)}, nil
	case len(lines) == 0:
		return &selfChange{summary: ss(l, "no_change", i18n.Args{"title": title})}, nil
	}
	return &selfChange{summary: strings.Join(lines, "\n"),
		apply: func(ctx context.Context, _ map[string]string) (string, error) {
			toolsBefore, out := h.assistantToolNames(ctx), ""
			if configures {
				p := h.extensions.reg.Snapshot(id)
				if p == nil {
					return "", fmt.Errorf("no extension %s", id)
				}
				now := h.savedSettings(p)
				// What the card showed is still so: one of these fields changed meanwhile (on another device) is not
				// overwritten with a value the user did not see next to it.
				for k := range patch {
					if !sameValue(now[k], before[k]) {
						return "", fmt.Errorf("%s changed since the card was shown; nothing saved: get it again", k)
					}
				}
				next, err := patchSettings(fields, now, patch)
				if err != nil {
					return "", err
				}
				raw, _ := json.Marshal(next)
				if err := h.checkExtensionSettings(p, raw); err != nil {
					return "", err
				}
				cleared, err := h.extensions.reg.SetConfigPreservingEntries(ctx, id, raw, "assistant")
				if err != nil {
					return "", settingsError(err)
				}
				out = title + " updated; " + name + " restarts with them."
				if len(cleared) > 0 {
					out += " Secrets bound to the old settings were deleted: " + strings.Join(cleared, ", ") + "."
				}
			}
			if switches { // as the Settings page switches it (switchExtension), which tells the conversations (toolsChanged)
				h.extensions.switchMu.Lock()
				err := h.setByUser(ctx, id, on, "assistant")
				h.extensions.switchMu.Unlock()
				if err != nil {
					return "", fmt.Errorf("%s was not switched %s: %w", strings.TrimSpace(out+" "+name), word, err)
				}
				out = strings.TrimSpace(out + " " + name + " is switched " + word + ".")
			}
			return out + toolChangeText(toolsBefore, h.assistantToolNames(ctx)), nil
		}}, nil
}

// checkExtensionSettings holds a whole configuration to the schema, and to what PUT …/-/config asks besides.
func (h *Manager) checkExtensionSettings(p *extension.Extension, raw json.RawMessage) error {
	if p.Record.ID == imessageExtensionID {
		if fe := h.imessageOneMac(raw); fe != nil {
			return errors.New(fe.Path + ": " + fe.Message)
		}
	}
	if err := p.Schema.CheckPreservingEntries(raw); err != nil {
		return settingsError(err)
	}
	return nil
}

// settingsError is a refused value for the model, in English.
func settingsError(err error) error {
	var fe *extension.FieldError
	if errors.As(err, &fe) {
		return fmt.Errorf("not saved: %s", fe.Text(i18n.En))
	}
	return err
}

// settingChanges are the card's lines, a field each in the form's order: its title, before → after; with the
// sentence the form asks before such a switch is turned on (x-confirm) when this turns it on.
func (h *Manager) settingChanges(l i18n.Lang, fields []extension.Field, before, after map[string]json.RawMessage, group string) []string {
	var lines []string
	for _, f := range fields {
		was, now := before[f.Name], after[f.Name]
		if sameValue(was, now) {
			continue
		}
		label := f.Title
		if label == "" {
			label = f.Name
		}
		if group != "" {
			label = group + " · " + label
		}
		if f.Type == "object" {
			b, a := map[string]json.RawMessage{}, map[string]json.RawMessage{}
			_ = json.Unmarshal(was, &b)
			_ = json.Unmarshal(now, &a)
			lines = append(lines, h.settingChanges(l, f.Fields, b, a, label)...)
			continue
		}
		lines = append(lines, changeSummary(l, label, h.fieldValueText(l, f, was), h.fieldValueText(l, f, now)))
		if f.Confirm != "" && turnsOn(f, was, now) {
			lines = append(lines, f.Confirm)
		}
	}
	return lines
}

// turnsOn: a switch going on, or a machine added to a list of them.
func turnsOn(f extension.Field, was, now json.RawMessage) bool {
	if f.Type == "boolean" {
		var b, a bool
		_ = json.Unmarshal(was, &b)
		_ = json.Unmarshal(now, &a)
		return a && !b
	}
	var b, a []string
	_ = json.Unmarshal(was, &b)
	_ = json.Unmarshal(now, &a)
	return slices.ContainsFunc(a, func(m string) bool { return !slices.Contains(b, m) })
}

// fieldValueText is a value as the card shows it, in l: a switch on or off, a choice by its title, a machine by its
// name, a list joined; a missing one is its default, said so.
func (h *Manager) fieldValueText(l i18n.Lang, f extension.Field, v json.RawMessage) string {
	if len(v) == 0 || isJSONNull(v) {
		if f.Default == nil {
			return ""
		}
		return i18n.T(l, "hub-self_settings_extensions.default", i18n.Args{"value": h.fieldValueText(l, f, f.Default)})
	}
	switch f.Type {
	case "boolean":
		var on bool
		_ = json.Unmarshal(v, &on)
		if on {
			return i18n.T(l, "hub-self_settings_extensions.on")
		}
		return i18n.T(l, "hub-self_settings_extensions.off")
	case "array":
		var items []json.RawMessage
		_ = json.Unmarshal(v, &items)
		if len(items) == 0 {
			return ""
		}
		item := extension.Field{Type: "string"}
		if f.Items != nil {
			item = *f.Items
		}
		texts := make([]string, 0, len(items))
		for _, it := range items {
			texts = append(texts, h.fieldValueText(l, item, it))
		}
		return strings.Join(texts, i18n.T(l, "hub-self_settings_extensions.list_separator"))
	}
	for _, c := range f.Enum {
		if sameValue(c.Value, v) && c.Title != "" {
			return c.Title
		}
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return string(v)
	}
	if f.Format == "machine" {
		return h.machineName(s)
	}
	return s
}

// assistantToolNames are the names of the tools the extensions offer the assistant now (AgentTools), sorted.
func (h *Manager) assistantToolNames(ctx context.Context) []string {
	agents, assistantOnly := h.AgentTools(ctx, true)
	names := make([]string, 0, len(agents)+len(assistantOnly))
	for _, t := range slices.Concat(agents, assistantOnly) {
		names = append(names, t.Name)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// toolChangeText says which tools a settings change gave or took away, so the conversation that made it knows what it
// can call now (turning ego on for a Mac brings browser_look and browser_run): "" when the change moved none. A
// connector that starts again may add its tools a moment later; the conversation's tool list follows by itself.
func toolChangeText(before, after []string) string {
	var gained, lost []string
	for _, n := range after {
		if !slices.Contains(before, n) {
			gained = append(gained, n)
		}
	}
	for _, n := range before {
		if !slices.Contains(after, n) {
			lost = append(lost, n)
		}
	}
	out := ""
	if len(gained) > 0 {
		out += " New tools: " + strings.Join(gained, ", ") + "."
	}
	if len(lost) > 0 {
		out += " Tools no longer offered: " + strings.Join(lost, ", ") + "."
	}
	if out != "" {
		out += " Running conversations are sent their new tool list."
	}
	return out
}
