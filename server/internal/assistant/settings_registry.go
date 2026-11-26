package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistant/saved"
	"github.com/nwverse/persona/server/internal/i18n"
)

// The table behind most of persona_settings' assistant keys: one settingField per key says what it is, how to read it
// (the value in effect), how to check a new one and how to save it; settingField.ownSetting turns it into the
// ownSetting the tool lists, reads and changes (the approval card says before → after, in the user's language). A new
// plain setting is one entry here plus its title in locales/*/assistant-settings_registry.json, not a new branch.

type fieldKind int

const (
	kindBool fieldKind = iota
	kindInt
	kindEnum
	kindStrings
	kindString
	kindJSON // decode says how
)

// settingField is one setting of the registry.
type settingField struct {
	key, about string
	kind       fieldKind
	min, max   int      // kindInt, inclusive
	enum       []string // kindEnum
	value      string   // what set takes, for the model; "" derives it from kind
	// decode reads a kindJSON value, and checks it.
	decode func(raw json.RawMessage) (any, error)
	// check refuses a value of the right kind that does not make sense (it may read the current state).
	check func(ctx context.Context, val any) error
	// get is the value in effect: a bool, int, string, []string, or for kindJSON anything json encodes.
	get func(ctx context.Context) (any, error)
	// write saves a checked value; the caller tells the apps (settingsChanged) when the write is to Settings.
	write func(ctx context.Context, val any) error
	// slow marks a get that costs a call to another computer: list leaves its value out.
	slow bool
}

func settingSlug(key string) string { return strings.NewReplacer(".", "_", "-", "_").Replace(key) }

func (f settingField) title(l i18n.Lang) string {
	return i18n.T(l, "assistant-settings_registry.title_"+settingSlug(f.key))
}

func (f settingField) schema() string {
	switch {
	case f.value != "":
		return f.value
	case f.kind == kindBool:
		return "true or false"
	case f.kind == kindInt:
		return fmt.Sprintf("a whole number from %d to %d", f.min, f.max)
	case f.kind == kindEnum:
		return strings.Join(f.enum, " or ")
	case f.kind == kindStrings:
		return "the whole list of strings"
	}
	return "a string"
}

// decodeValue reads a value of the field's kind, refusing null and anything of another kind.
func (f settingField) decodeValue(raw json.RawMessage) (any, error) {
	bad := fmt.Errorf("value must be %s", f.schema())
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, bad
	}
	switch f.kind {
	case kindBool:
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, bad
		}
		return v, nil
	case kindInt:
		var v int
		if err := json.Unmarshal(raw, &v); err != nil || v < f.min || v > f.max {
			return nil, bad
		}
		return v, nil
	case kindEnum:
		var v string
		if err := json.Unmarshal(raw, &v); err != nil || !slices.Contains(f.enum, v) {
			return nil, bad
		}
		return v, nil
	case kindStrings:
		var v []string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, bad
		}
		if v == nil {
			v = []string{}
		}
		return v, nil
	case kindJSON:
		return f.decode(raw)
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, bad
	}
	return v, nil
}

// plain is a value as the model reads it.
func plain(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool, int:
		return fmt.Sprint(x)
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}

// shown is a value as the approval card says it, in l.
func (f settingField) shown(l i18n.Lang, v any) string {
	if b, ok := v.(bool); ok {
		if b {
			return i18n.T(l, "assistant-settings_registry.on")
		}
		return i18n.T(l, "assistant-settings_registry.off")
	}
	return clipped(l, plain(v))
}

func (s *Service) ownSetting(f settingField) ownSetting {
	return ownSetting{
		Setting: Setting{Key: f.key, Title: f.title(i18n.Default), Actions: []string{"get", "set"}, About: f.about, Value: f.schema()},
		get: func(ctx context.Context) (string, error) {
			v, err := f.get(ctx)
			if err != nil {
				return "", err
			}
			return orNone(plain(v)), nil
		},
		set:       func(ctx context.Context, raw json.RawMessage) (*ownChange, error) { return s.setField(ctx, f, raw) },
		cardTitle: f.title,
		noCurrent: f.slow,
	}
}

// setField is a registry setting's set: checked before any card, a no-op when the value is the one in effect.
func (s *Service) setField(ctx context.Context, f settingField, raw json.RawMessage) (*ownChange, error) {
	val, err := f.decodeValue(raw)
	if err != nil {
		return nil, err
	}
	if f.check != nil {
		if err := f.check(ctx, val); err != nil {
			return nil, err
		}
	}
	before, err := f.get(ctx)
	if err != nil {
		return nil, err
	}
	if plain(before) == plain(val) {
		return &ownChange{summary: f.key + " is unchanged"}, nil
	}
	return &ownChange{
		cardText: func(l i18n.Lang) string {
			return i18n.T(l, "assistant-settings_registry.change", i18n.Args{"title": f.title(l), "before": f.shown(l, before), "after": f.shown(l, val)})
		},
		apply: func(ctx context.Context) (string, error) {
			if err := f.write(ctx, val); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s is now %s.", f.key, clip(oneLine(plain(val)), 200)), nil
		},
	}, nil
}

func refOf[T any](v T) *T { return &v }

// profileWrite saves into Settings and tells the apps.
func (s *Service) profileWrite(put func(v *Settings, val any)) func(context.Context, any) error {
	return func(ctx context.Context, val any) error {
		if _, err := s.updateSettings(ctx, func(v *Settings) error { put(v, val); return nil }); err != nil {
			return err
		}
		s.settingsChanged(ctx)
		return nil
	}
}

func (s *Service) fromSettings(read func(v Settings) any) func(context.Context) (any, error) {
	return func(ctx context.Context) (any, error) { return read(s.settings(ctx)), nil }
}

// registrySettings are the registry's settings as the tool offers them.
func (s *Service) registrySettings() []ownSetting {
	var out []ownSetting
	for _, fields := range [][]settingField{s.profileFields(), s.triageFields()} {
		for _, f := range fields {
			out = append(out, s.ownSetting(f))
		}
	}
	return append(out, s.savedPromptsSetting())
}

// profileFields are the assistant's profile settings (GET / PUT /api/v1/assistant) that persona_settings did not
// have: each reads the value in effect and writes the saved field, as the route does.
func (s *Service) profileFields() []settingField {
	boolIn := func(of func(Settings) *bool, def bool) func(Settings) any {
		return func(v Settings) any {
			if p := of(v); p != nil {
				return *p
			}
			return def
		}
	}
	return []settingField{
		{key: "assistant.cache_keepalive", kind: kindBool, about: "on (the default): Persona keeps the prompt cache of the main conversation warm with a small request " +
			"before it expires, so the next message is read from the cache instead of rewritten; costs a little per request. Applies at once",
			get: s.fromSettings(func(v Settings) any { return cacheKeepaliveOf(v).Enabled }), write: s.cacheKeepaliveWrite(func(v *Settings, val any) { v.CacheKeepalive = refOf(val.(bool)) })},
		{key: "assistant.cache_keepalive_minutes", kind: kindInt, min: 1, max: 55, about: "minutes between cache keep-alive requests; 55 by default (the cache lives an hour)",
			get: s.fromSettings(func(v Settings) any { return cacheKeepaliveOf(v).IntervalMinutes }), write: s.cacheKeepaliveWrite(func(v *Settings, val any) { v.CacheKeepaliveMinutes = refOf(val.(int)) })},
		{key: "assistant.cache_keepalive_max", kind: kindInt, min: 1, max: 144, about: "most keep-alive requests in a row while nobody writes; 24 by default",
			get: s.fromSettings(func(v Settings) any { return cacheKeepaliveOf(v).MaxRequests }), write: s.cacheKeepaliveWrite(func(v *Settings, val any) { v.CacheKeepaliveMax = refOf(val.(int)) })},
		{key: "assistant.idle_close_hours", kind: kindInt, min: 0, max: maxIdleCloseHours, about: "hours an Agent Persona started may sit idle with nothing going on before it is closed; " +
			"0 turns this off, 12 by default",
			get: func(ctx context.Context) (any, error) { return int(s.idleCloseAfter(ctx) / time.Hour), nil }, write: s.profileWrite(func(v *Settings, val any) { v.IdleCloseHours = refOf(val.(int)) })},
		{key: "assistant.archive_closed_days", kind: kindInt, min: 0, max: maxArchiveClosedDays, about: "days a closed Agent session sits idle, on no to-do or only on done ones, before it is archived by itself; " +
			"0 turns this off, 3 by default",
			get: func(ctx context.Context) (any, error) { return int(s.archiveClosedAfter(ctx) / (24 * time.Hour)), nil }, write: s.profileWrite(func(v *Settings, val any) { v.ArchiveClosedDays = refOf(val.(int)) })},
		{key: "assistant.agent_auto_naming", kind: kindBool, about: "on (the default): Persona renames an Agent's tab whose name is only a placeholder from what it works on; names the user gave are never changed",
			get: s.fromSettings(func(v Settings) any { return quietOn(v.AgentAutoNaming) }), write: s.profileWrite(func(v *Settings, val any) { v.AgentAutoNaming = refOf(val.(bool)) })},
		{key: "assistant.agent_auto_naming_max", kind: kindInt, min: 1, max: maxAgentNamingChanges, about: "most automatic name changes per Agent session; 1 by default",
			get: s.fromSettings(func(v Settings) any { return agentNamingLimit(v) }), write: s.profileWrite(func(v *Settings, val any) { v.AgentAutoNamingMax = refOf(val.(int)) })},
		{key: "assistant.agent_auto_naming_interval_hours", kind: kindInt, min: 0, max: maxAgentNamingInterval, about: "hours after which new output makes Persona look at an Agent's name again; 0 (the default) is off",
			get: s.fromSettings(func(v Settings) any { return agentNamingInterval(v) }), write: s.profileWrite(func(v *Settings, val any) { v.AgentAutoNamingIntervalHours = refOf(val.(int)) })},
		{key: "assistant.next_suggestions", kind: kindBool, about: "off (the default): on, a chat without a suggestion of its own (Persona's conversations, Codex and the other runtimes) gets the next message suggested after each turn; costs a model call per turn",
			get: s.fromSettings(boolIn(func(v Settings) *bool { return v.NextSuggestions }, false)), write: s.profileWrite(func(v *Settings, val any) { v.NextSuggestions = refOf(val.(bool)) })},
		{key: "assistant.inbox_post", kind: kindBool, about: "on (the default): the assistant may put a conversation into the Inbox (inbox_post, a task's put-it-in-the-Inbox-when-done); a row still comes only from the user's own request or a task they set",
			get: func(ctx context.Context) (any, error) { return s.inboxPostOn(ctx), nil }, write: s.profileWrite(func(v *Settings, val any) { v.InboxPost = refOf(val.(bool)) })},
		{key: "assistant.owner_reply_approval", kind: kindBool, about: "off (the default): on, the user's own exact reply in chat (one of assistant.owner_reply_approval_phrases) approves the connector batch Persona just prepared, once; " +
			"it never creates an Always rule",
			get: s.fromSettings(func(v Settings) any { return ownerReplyApprovalOn(v) }), write: s.profileWrite(func(v *Settings, val any) { v.OwnerReplyApproval = refOf(val.(bool)) })},
		{key: "assistant.persistent_owner_action_cards", kind: kindBool, about: "off (the default): on, a prepared action card that can be rebuilt stays pending until the user answers instead of expiring",
			get: s.fromSettings(func(v Settings) any { return persistentOwnerActionCardsOn(v) }), write: s.profileWrite(func(v *Settings, val any) { v.PersistentOwnerActionCards = refOf(val.(bool)) })},
		{key: "assistant.owner_reply_approval_phrases", kind: kindStrings, about: "the exact replies assistant.owner_reply_approval accepts, at most 8, each 1–32 characters, unique; [] turns reply approval off " +
			"without touching ordinary Allow / Deny cards",
			value: "the whole list of phrases, [] for none",
			check: func(_ context.Context, val any) error {
				_, err := ownerApprovalPhrases(Settings{OwnerApprovalPhrases: val.([]string)})
				return err
			},
			get:   s.fromSettings(func(v Settings) any { return ownerApprovalPhrasesForView(v) }),
			write: s.profileWrite(func(v *Settings, val any) { v.OwnerApprovalPhrases = val.([]string) })},
		s.machineField(),
	}
}

// cacheKeepaliveWrite saves a keep-alive field and, when the policy in effect changed, drops the requests already captured
// (as PUT /api/v1/assistant does): a genuine request arms the new policy.
func (s *Service) cacheKeepaliveWrite(put func(v *Settings, val any)) func(context.Context, any) error {
	save := s.profileWrite(put)
	return func(ctx context.Context, val any) error {
		before := s.settings(ctx)
		if err := save(ctx, val); err != nil {
			return err
		}
		if cacheKeepaliveOf(before) != cacheKeepaliveOf(s.settings(ctx)) {
			clearCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			for _, ref := range s.cacheKeepaliveRefs(ctx, before) {
				_ = s.call(clearCtx, ref.MachineID, "assistant.cache_clear", map[string]any{"session_id": ref.SessionID}, nil)
			}
		}
		return nil
	}
}

// machineField is where the main chat runs: the computer its conversations start on (the launcher is assistant.launcher).
func (s *Service) machineField() settingField {
	return settingField{key: "assistant.machine", kind: kindString, about: "the computer (machine id, from the Devices list) the assistant's conversations run on; " +
		"new conversations start there. A computer the user set aside for work cannot be chosen",
		value: "a machine id",
		check: func(ctx context.Context, val any) error {
			id := val.(string)
			if _, ok := s.device(ctx, id); !ok {
				return errors.New("unknown machine")
			}
			if s.isWorkMachine(id) {
				return i18n.Err("assistant-start.work_machine")
			}
			return nil
		},
		get: s.fromSettings(func(v Settings) any { return v.MachineID }),
		write: func(ctx context.Context, val any) error {
			id := val.(string)
			before := s.settings(ctx)
			mainMachine := s.confirmedMainMachine(ctx, before)
			if mainMachine == "" && before.Main != "" {
				mainMachine = before.MachineID // keep the legacy route while its inventory is missing
			}
			return s.profileWrite(func(v *Settings, _ any) {
				v.MachineID = id
				if v.Main == before.Main && v.MainMachineID == "" {
					v.MainMachineID = mainMachine
				}
			})(ctx, val)
		}}
}

// triageFields are the session triage's and close-out's switches (GET / PUT /api/v1/agents/triage): each key changes one,
// the rest stay as saved.
func (s *Service) triageFields() []settingField {
	field := func(name, about string, kind fieldKind, get func(TriageSwitches) any, put func(*TriageSwitches, any)) settingField {
		f := settingField{key: "agents.triage." + name, kind: kind, about: about,
			get: func(ctx context.Context) (any, error) {
				sw, err := s.readTriageSwitches(ctx)
				if err != nil {
					return nil, err
				}
				return get(sw), nil
			},
			write: func(ctx context.Context, val any) error {
				sw, err := s.readTriageSwitches(ctx)
				if err != nil {
					return err
				}
				put(&sw, val)
				return s.putTriageSwitches(ctx, sw)
			}}
		if kind == kindInt {
			f.min, f.max = 0, 10080
		}
		return f
	}
	b := func(get func(TriageSwitches) bool, put func(*TriageSwitches, bool)) (func(TriageSwitches) any, func(*TriageSwitches, any)) {
		return func(sw TriageSwitches) any { return get(sw) }, func(sw *TriageSwitches, v any) { put(sw, v.(bool)) }
	}
	var out []settingField
	add := func(name, about string, get func(TriageSwitches) bool, put func(*TriageSwitches, bool)) {
		g, p := b(get, put)
		out = append(out, field(name, about, kindBool, g, p))
	}
	add("enabled", "off (the default): on, Persona triages every stopped Agent turn with a model (the lists' status line, done no longer unread); needed by the other agents.triage switches except transient_retry and reply_reminder. Costs a model call per stopped turn",
		func(sw TriageSwitches) bool { return sw.Enabled }, func(sw *TriageSwitches, v bool) { sw.Enabled = v })
	add("auto_continue", "off (the default): on, Persona sends “Continue” to an Agent after a usage limit resets (needs agents.triage.enabled); the Agent runs a turn on its own model",
		func(sw TriageSwitches) bool { return sw.AutoContinue }, func(sw *TriageSwitches, v bool) { sw.AutoContinue = v })
	add("wake", "off (the default): on, Persona wakes an Agent whose wait should be over (needs agents.triage.enabled)",
		func(sw TriageSwitches) bool { return sw.Wake }, func(sw *TriageSwitches, v bool) { sw.Wake = v })
	add("decide", "off (the default): on, Persona makes a card for what the user is to decide (needs agents.triage.enabled)",
		func(sw TriageSwitches) bool { return sw.Decide }, func(sw *TriageSwitches, v bool) { sw.Decide = v })
	add("tool_loop", "off (the default): on, Persona notices an Agent repeating the same tool calls while a turn runs and asks the user",
		func(sw TriageSwitches) bool { return sw.ToolLoop }, func(sw *TriageSwitches, v bool) { sw.ToolLoop = v })
	add("closeout", "off (the default): on, an Agent whose turn did work and ended without a close-out is held once and asked for one; the held stop runs one more turn on its own model",
		func(sw TriageSwitches) bool { return sw.Closeout }, func(sw *TriageSwitches, v bool) { sw.Closeout = v })
	add("closeout_ask", "off (the default): on, an Agent nobody supervises is asked once where its work stands when a turn that did work ended without a close-out and triage could not call it finished (needs agents.triage.enabled)",
		func(sw TriageSwitches) bool { return sw.CloseoutAsk }, func(sw *TriageSwitches, v bool) { sw.CloseoutAsk = v })
	add("reply_reminder", "on (the default): push once when an Agent the user looked at still waits for their answer; rules only, no model calls",
		func(sw TriageSwitches) bool { return sw.ReplyReminder == nil || *sw.ReplyReminder }, func(sw *TriageSwitches, v bool) { sw.ReplyReminder = refOf(v) })
	add("transient_retry", "on (the default): Persona sends “Continue” after a transient model error (a dropped connection, a 5xx, overloaded) with growing delays; no model call, independent of agents.triage.enabled",
		func(sw TriageSwitches) bool { return sw.transientOn() }, func(sw *TriageSwitches, v bool) { sw.TransientRetry = refOf(v) })
	n := field("reply_reminder_minutes", "minutes since the user looked before agents.triage.reply_reminder pushes; 30 by default (0 puts it back to that)", kindInt,
		func(sw TriageSwitches) any {
			if sw.ReplyReminderMinutes == 0 {
				return 30
			}
			return sw.ReplyReminderMinutes
		}, func(sw *TriageSwitches, v any) { sw.ReplyReminderMinutes = v.(int) })
	n.min = 0
	return append(out, n)
}

// savedPromptsSetting is the user's own "/" prompts (Settings › Saved Prompts used to edit them): get lists them, add
// saves one, set changes one by id, remove deletes one by id.
func (s *Service) savedPromptsSetting() ownSetting {
	const savedPromptsKey = "assistant.saved_prompts"
	list := func(ctx context.Context) ([]saved.Prompt, error) { return s.savedPrompts.List(ctx) }
	byID := func(ctx context.Context, id string) (saved.Prompt, error) {
		prompts, err := list(ctx)
		if err != nil {
			return saved.Prompt{}, err
		}
		i := slices.IndexFunc(prompts, func(p saved.Prompt) bool { return p.ID == id })
		if i < 0 {
			return saved.Prompt{}, fmt.Errorf("no saved prompt %q (get lists them)", id)
		}
		return prompts[i], nil
	}
	read := func(raw json.RawMessage, withID bool) (id, name, text string, err error) {
		var in struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Text string `json:"text"`
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&in); err != nil {
			return "", "", "", errors.New(`value must be {"name": "word", "text": "what it types"}` + map[bool]string{true: ` with the prompt's "id"`}[withID])
		}
		if withID && in.ID == "" {
			return "", "", "", errors.New(`value needs the prompt's "id" (get lists them)`)
		}
		p, err := saved.Check(in.Name, in.Text)
		return in.ID, p.Name, p.Text, err
	}
	cardText := func(k string, args i18n.Args) func(l i18n.Lang) string {
		return func(l i18n.Lang) string { return i18n.T(l, "assistant-settings_registry."+k, args) }
	}
	return ownSetting{
		Setting: Setting{Key: savedPromptsKey, Title: i18n.T(i18n.Default, "assistant-settings_registry.title_assistant_saved_prompts"), Actions: []string{"get", "add", "set", "remove"},
			About: "the user's own “/” prompts: the instructions they send again and again; every chat's “/” list offers them after the built-in commands " +
				"and picking one fills the composer, it never sends. Names are one word (letters, digits, - _ .), at most 32 characters; text at most 8000; at most 100 prompts",
			Value: `add: {"name", "text"}; set: {"id", "name", "text"} changes one; remove: the prompt's id (from get)`},
		cardTitle: func(l i18n.Lang) string {
			return i18n.T(l, "assistant-settings_registry.title_assistant_saved_prompts")
		},
		get: func(ctx context.Context) (string, error) {
			prompts, err := list(ctx)
			if err != nil {
				return "", err
			}
			if len(prompts) == 0 {
				return "（无）", nil // i18n-ignore: the settings tool's reply to the model, not shown to people
			}
			var b strings.Builder
			for _, p := range prompts {
				fmt.Fprintf(&b, "- %s /%s: %s\n", p.ID, p.Name, clip(oneLine(p.Text), 200))
			}
			return b.String(), nil
		},
		set: func(ctx context.Context, raw json.RawMessage) (*ownChange, error) {
			id, name, text, err := read(raw, true)
			if err != nil {
				return nil, err
			}
			old, err := byID(ctx, id)
			if err != nil {
				return nil, err
			}
			if old.Name == name && old.Text == text {
				return &ownChange{summary: "the saved prompt is unchanged"}, nil
			}
			return &ownChange{cardText: func(l i18n.Lang) string {
				return i18n.T(l, "assistant-settings_registry.prompt_set", i18n.Args{"name": name, "before": clipped(l, old.Text), "after": clipped(l, text)})
			}, apply: func(ctx context.Context) (string, error) {
				if _, err := s.savedPrompts.Update(ctx, id, name, text); err != nil {
					return "", err
				}
				return "Saved prompt /" + name + " changed.", nil
			}}, nil
		},
		change: func(ctx context.Context, action string, raw json.RawMessage) (*ownChange, error) {
			switch action {
			case "add":
				_, name, text, err := read(raw, false)
				if err != nil {
					return nil, err
				}
				prompts, err := list(ctx)
				if err != nil {
					return nil, err
				}
				if slices.ContainsFunc(prompts, func(p saved.Prompt) bool { return strings.EqualFold(p.Name, name) }) {
					return nil, fmt.Errorf("a saved prompt named /%s exists (set changes it)", name)
				}
				return &ownChange{cardText: func(l i18n.Lang) string {
					return i18n.T(l, "assistant-settings_registry.prompt_add", i18n.Args{"name": name, "text": clipped(l, text)})
				}, apply: func(ctx context.Context) (string, error) {
					if _, err := s.savedPrompts.Add(ctx, name, text); err != nil {
						return "", err
					}
					return "Saved prompt /" + name + " added.", nil
				}}, nil
			case "remove":
				var id string
				if err := json.Unmarshal(raw, &id); err != nil || id == "" {
					return nil, errors.New("remove takes the prompt's id (get lists them)")
				}
				old, err := byID(ctx, id)
				if err != nil {
					return nil, err
				}
				return &ownChange{cardText: cardText("prompt_remove", i18n.Args{"name": old.Name}), apply: func(ctx context.Context) (string, error) {
					if err := s.savedPrompts.Delete(ctx, id); err != nil {
						return "", err
					}
					return "Saved prompt /" + old.Name + " removed.", nil
				}}, nil
			}
			return nil, fmt.Errorf("%s does not take %q", savedPromptsKey, action)
		},
	}
}
