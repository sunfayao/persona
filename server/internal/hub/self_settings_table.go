package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/update"
)

// More of the hub's own settings for persona_settings, from a table (the update channel and auto-update; the shape of
// each is hubValue), and the two that are lists of something per device or per computer: a device's notifications and
// each agent CLI's own settings. Every change reuses what the app's route does, so a value is held to the same checks.

// hubValue is one plain setting: a boolean or one of a few words.
type hubValue struct {
	key, about string
	enum       []string // nil: a boolean
	get        func(ctx context.Context) (string, error)
	put        func(ctx context.Context, val string) error
}

func tableText(l i18n.Lang, name string, args ...i18n.Args) string {
	return i18n.T(l, "hub-self_settings_table."+name, args...)
}

func tableSlug(key string) string { return strings.NewReplacer(".", "_", "-", "_").Replace(key) }

func (h *Hub) valueSetting(v hubValue) selfSetting {
	schema := "true or false"
	if v.enum != nil {
		schema = strings.Join(v.enum, " or ")
	}
	title := func(l i18n.Lang) string { return tableText(l, "title_"+tableSlug(v.key)) }
	return selfSetting{key: v.key, title: title(h.userLang()), about: v.about, value: schema, get: v.get,
		set: func(ctx context.Context, raw json.RawMessage) (*selfChange, error) {
			var val string
			if v.enum == nil {
				var b bool
				if err := json.Unmarshal(raw, &b); err != nil {
					return nil, errors.New("value must be true or false")
				}
				val = fmt.Sprint(b)
			} else if err := json.Unmarshal(raw, &val); err != nil || !slices.Contains(v.enum, val) {
				return nil, fmt.Errorf("value must be %s", schema)
			}
			before, err := v.get(ctx)
			if err != nil {
				return nil, err
			}
			l := h.userLang()
			if before == val {
				return &selfChange{summary: tableText(l, "no_change", i18n.Args{"title": title(l)})}, nil
			}
			show := func(s string) string {
				if v.enum == nil {
					return tableText(l, map[string]string{"true": "on", "false": "off"}[s])
				}
				return s
			}
			return &selfChange{summary: tableText(l, "change", i18n.Args{"title": title(l), "before": show(before), "after": show(val)}),
				apply: func(ctx context.Context, _ map[string]string) (string, error) {
					if err := v.put(ctx, val); err != nil {
						return "", err
					}
					return v.key + " is now " + val + ".", nil
				}}, nil
		}}
}

// putUpdateSettings changes the hub's update settings through the route's own handler (PUT /api/v1/hub/update/settings),
// so a value meets the same checks; a refusal is returned as the route words it.
func (h *Hub) putUpdateSettings(ctx context.Context, body map[string]any) error {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "/api/v1/hub/update/settings", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.upd.putSettings(rec, req)
	if rec.Code < 300 {
		return nil
	}
	var e struct {
		Error string `json:"error"`
	}
	b, _ := io.ReadAll(rec.Body)
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		return errors.New(e.Error)
	}
	return fmt.Errorf("the hub refused the change (HTTP %d)", rec.Code)
}

func (h *Hub) updateNow(ctx context.Context) (updateView, error) {
	if h.upd == nil {
		return updateView{}, errors.New("this hub does not update itself")
	}
	if err := h.upd.load(ctx); err != nil {
		return updateView{}, err
	}
	return h.upd.view(), nil
}

func (h *Hub) updateSettingsSelf() []selfSetting {
	if h.upd == nil {
		return nil
	}
	flag := func(field string, read func(updateView) bool) (func(context.Context) (string, error), func(context.Context, string) error) {
		return func(ctx context.Context) (string, error) {
				v, err := h.updateNow(ctx)
				return fmt.Sprint(read(v)), err
			}, func(ctx context.Context, val string) error {
				return h.putUpdateSettings(ctx, map[string]any{field: val == "true"})
			}
	}
	autoGet, autoPut := flag("auto", func(v updateView) bool { return v.Auto })
	nodesGet, nodesPut := flag("auto_update_nodes", func(v updateView) bool { return v.AutoUpdateNodes })
	return []selfSetting{
		h.valueSetting(hubValue{key: "hub.update_channel", enum: []string{"release", update.ChannelBeta, "web-dev"},
			about: "which builds the hub updates to: release (the default), beta (pre-releases), or web-dev (the web app's development builds; needs a development setup and is refused otherwise)",
			get: func(ctx context.Context) (string, error) {
				v, err := h.updateNow(ctx)
				return v.Source, err
			},
			put: func(ctx context.Context, val string) error {
				return h.putUpdateSettings(ctx, map[string]any{"source": val})
			}}),
		h.valueSetting(hubValue{key: "hub.auto_update", about: "on (the default): the hub installs a new release by itself once the assistant and the Agents are idle; off: it only tells the user one is there", get: autoGet, put: autoPut}),
		h.valueSetting(hubValue{key: "hub.auto_update_nodes", about: "on: the personad on the user's computers is updated together with the hub", get: nodesGet, put: nodesPut}),
	}
}

// ---- a device's notifications ----

func (p *Pusher) deviceList() []Device {
	p.mu.Lock()
	out := make([]Device, 0, len(p.devices))
	for _, d := range p.devices {
		out = append(out, d)
	}
	p.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name+out[i].DeviceID < out[j].Name+out[j].DeviceID })
	return out
}

// devicePrefsSetting is notifications.devices: every device's notification choices (the app's Settings › Notifications,
// as GET / PUT /api/v1/devices/{id}/prefs keeps them).
func (h *Hub) devicePrefsSetting() selfSetting {
	title := tableText(h.userLang(), "title_notifications_devices")
	return selfSetting{key: "notifications.devices", title: title,
		about: "which pushes each of the user's devices wants: input (questions, plans, permissions), done (off, long: turns of at least the hub's long-turn seconds, or always), " +
			"mute_while_desktop, quiet hours and muted managers. get lists every device with its id",
		value: `{"device_id": from get, and only the fields to change: "input": bool, "done": "off|long|always", "mute_while_desktop": bool, ` +
			`"quiet": {"enabled": bool, "start": "HH:MM", "end": "HH:MM", "tz"?: IANA name}, "muted_managers": [manager ids]}`,
		get: func(context.Context) (string, error) {
			var b strings.Builder
			for _, d := range h.push.deviceList() {
				raw, _ := json.Marshal(h.prefsView(devicePrefs(d.Prefs)))
				fmt.Fprintf(&b, "- %s (%s, %s): %s\n", d.DeviceID, d.Name, d.Kind, raw)
			}
			if b.Len() == 0 {
				return "No devices.", nil
			}
			return b.String(), nil
		},
		set: func(ctx context.Context, raw json.RawMessage) (*selfChange, error) {
			var in map[string]json.RawMessage
			if err := json.Unmarshal(raw, &in); err != nil || len(in) < 2 {
				return nil, errors.New(`value must be {"device_id": …, plus the fields to change}`)
			}
			var id string
			if err := json.Unmarshal(in["device_id"], &id); err != nil || id == "" {
				return nil, errors.New(`value needs "device_id" (get lists them)`)
			}
			delete(in, "device_id")
			dev, ok := h.push.deviceByID(id)
			if !ok {
				return nil, fmt.Errorf("no device %q (get lists them)", id)
			}
			patch, _ := json.Marshal(in)
			// held to the route's checks on a copy; the write below repeats them on the stored prefs
			edit := func(prefs *NotifyPrefs) error {
				if err := json.Unmarshal(patch, prefs); err != nil {
					return err
				}
				if err := prefs.Quiet.validate(); err != nil {
					return err
				}
				prefs.MutedManagers = h.rosterManagers(ctx, prefs.MutedManagers)
				return nil
			}
			now := h.push.prefs(id)
			probe := now
			if err := edit(&probe); err != nil {
				return nil, err
			}
			l := h.userLang()
			if bytes.Equal(mustJSON(devicePrefs(mustJSON(probe))), mustJSON(now)) {
				return &selfChange{summary: tableText(l, "no_change", i18n.Args{"title": title})}, nil
			}
			return &selfChange{summary: tableText(l, "device_prefs", i18n.Args{"device": dev.Name, "changes": string(patch)}),
				apply: func(ctx context.Context, _ map[string]string) (string, error) {
					if _, err := h.push.editPrefs(id, dev.Credential, edit); err != nil {
						return "", err
					}
					return "Notifications of " + dev.Name + " saved.", nil
				}}, nil
		}}
}

func mustJSON(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }

func (p *Pusher) deviceByID(id string) (Device, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	d, ok := p.devices[id]
	return d, ok
}

// ---- each agent CLI's own settings on a computer ----

// agentCLIs are the agents whose settings file the node keeps a whitelist of (the routes /api/v1/machines/{mid}/<cli>-settings).
var agentCLIs = []string{"claude", "codex", "agy", "omp", "grok"}

// agentCLISetting is agent_settings.<cli>: that agent's own settings on one computer (Settings › Claude Code / Codex).
func (h *Hub) agentCLISetting(cli string) selfSetting {
	title := tableText(h.userLang(), "title_agent_settings_"+cli)
	read := func(ctx context.Context, m *Machine) ([]map[string]any, error) {
		var out struct {
			Settings []map[string]any `json:"settings"`
		}
		params := map[string]any{}
		if cli == "claude" {
			params["locale"] = h.userLang()
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := m.call(cctx, cli+".settings", params, &out); err != nil {
			return nil, err
		}
		return out.Settings, nil
	}
	return selfSetting{key: "agent_settings." + cli, title: title,
		about: "the settings of " + cli + " itself (its own settings file) on one of the user's computers, from the node's whitelist; get lists each computer's settings " +
			"with their keys, choices and values. Applies to the agent's next session",
		value: `{"machine_id": from get, "key": a setting's key from get, "value": its new value, or null for the agent's default}`,
		get: func(ctx context.Context) (string, error) {
			var b strings.Builder
			for _, m := range h.machinesNow() {
				if h.machineUntrusted(m.cfg.ID) {
					continue
				}
				rows, err := read(ctx, m)
				if err != nil {
					fmt.Fprintf(&b, "%s (%s): unavailable (%v)\n", m.cfg.ID, m.name(), err)
					continue
				}
				fmt.Fprintf(&b, "%s (%s):\n", m.cfg.ID, m.name())
				for _, r := range rows {
					raw, _ := json.Marshal(r)
					fmt.Fprintf(&b, "  - %s\n", raw)
				}
			}
			if b.Len() == 0 {
				return "No computers.", nil
			}
			return b.String(), nil
		},
		set: func(ctx context.Context, raw json.RawMessage) (*selfChange, error) {
			var in struct {
				MachineID string          `json:"machine_id"`
				Key       string          `json:"key"`
				Value     json.RawMessage `json:"value"`
			}
			if err := json.Unmarshal(raw, &in); err != nil || in.MachineID == "" || in.Key == "" || len(in.Value) == 0 {
				return nil, errors.New(`value must be {"machine_id", "key", "value"} (get lists them)`)
			}
			m := h.localMachine(in.MachineID)
			if m == nil || h.machineUntrusted(m.cfg.ID) {
				return nil, fmt.Errorf("no computer %q", in.MachineID)
			}
			rows, err := read(ctx, m)
			if err != nil {
				return nil, err
			}
			i := slices.IndexFunc(rows, func(r map[string]any) bool { return r["key"] == in.Key })
			if i < 0 {
				return nil, fmt.Errorf("%s has no setting %q on %s (get lists them)", cli, in.Key, in.MachineID)
			}
			before, _ := json.Marshal(rows[i]["value"])
			l := h.userLang()
			if bytes.Equal(before, bytes.TrimSpace(in.Value)) {
				return &selfChange{summary: tableText(l, "no_change", i18n.Args{"title": title})}, nil
			}
			return &selfChange{summary: tableText(l, "agent_setting", i18n.Args{"agent": cli, "key": in.Key, "machine": m.name(), "before": string(before), "after": string(in.Value)}),
				apply: func(ctx context.Context, _ map[string]string) (string, error) {
					params := map[string]any{"key": in.Key, "value": in.Value}
					if cli == "claude" {
						params["locale"] = l
					}
					cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
					defer cancel()
					var out json.RawMessage
					if err := m.call(cctx, cli+".settings.set", params, &out); err != nil {
						return "", err
					}
					return fmt.Sprintf("%s %s is set on %s.", cli, in.Key, m.name()), nil
				}}, nil
		}}
}
