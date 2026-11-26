package node

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nwverse/persona/server/internal/tomltext"
)

// Codex's options the app may change ("Settings › Codex Settings"), in ~/.codex/config.toml. Only these top-level keys are read
// or written, line by line: the rest of the file (comments, [model_providers.*], [projects.*]…) stays byte for byte.
// The same shape as Claude's (claudeconfig.go), so the app draws both with one page.
var codexSettings = []ClaudeSetting{
	{Key: "model", Title: "模型", Detail: "没指定模型时 Codex 用哪个，比如 gpt-5.5", Type: "string", Default: ""},
	{Key: "model_reasoning_effort", Title: "思考强度", Detail: "越高越慢、越仔细", Type: "enum", Options: codexEffortLevels, Default: "medium"},
	{Key: "service_tier", Title: "Speed", Detail: "Fast increases usage. Applies to new Codex sessions on supported models.", Type: "enum", Options: []string{"default", "fast"}, Default: "default"},
	{Key: "model_reasoning_summary", Title: "思考摘要", Detail: "把模型的思考整理成摘要显示，也可以不显示", Type: "enum", Options: []string{"auto", "concise", "detailed", "none"}, Default: "auto"},
	{Key: "model_verbosity", Title: "回答详细程度", Detail: "回答写多长", Type: "enum", Options: []string{"low", "medium", "high"}, Default: "medium"},
	{Key: "approval_policy", Title: "什么时候问你", Detail: "执行命令前要不要先问你，也可以一律不问", Type: "enum", Options: []string{"untrusted", "on-failure", "on-request", "never"}, Default: "on-request"},
	{Key: "sandbox_mode", Title: "沙箱", Detail: "命令能读写哪些地方，也可以完全不设限", Type: "enum", Options: []string{"read-only", "workspace-write", "danger-full-access"}, Default: "read-only"},
	{Key: "hide_agent_reasoning", Title: "隐藏思考过程", Detail: "不显示模型的思考事件", Type: "bool", Default: false},
	{Key: "show_raw_agent_reasoning", Title: "显示原始思考", Detail: "模型给出原始思考内容时照原样显示", Type: "bool", Default: false},
}

func (n *Node) codexConfigPath() string {
	p := filepath.Join(n.codexHome(), "config.toml")
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real // a symlink into a dotfiles repo: edit the target
	}
	return p
}

func (n *Node) getCodexSettings() ([]ClaudeSetting, error) {
	b, err := os.ReadFile(n.codexConfigPath())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	vals := tomltext.TopLevel(string(b))
	out := make([]ClaudeSetting, len(codexSettings))
	for i, s := range codexSettings {
		if v, ok := vals[s.Key]; ok {
			// Both spellings occur in native configurations.
			if s.Key == "service_tier" && v == "priority" {
				v = "fast"
			}
			s.Value = v
		}
		out[i] = s
	}
	return out, nil
}

func (n *Node) setCodexSetting(key string, value any) error {
	var def *ClaudeSetting
	for i := range codexSettings {
		if codexSettings[i].Key == key {
			def = &codexSettings[i]
		}
	}
	if def == nil {
		return fmt.Errorf("setting %q is not editable from the app", key)
	}
	if err := checkSettingValue(*def, value); err != nil {
		return err
	}
	path := n.codexConfigPath()
	agentConfigMu.Lock()
	defer agentConfigMu.Unlock()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := tomltext.Set(string(b), key, value)
	return writeConfigFile(path, []byte(text), 0o600)
}

// checkSettingValue: a value of the option's type (nil = back to the default).
func checkSettingValue(def ClaudeSetting, value any) error {
	if value == nil {
		return nil
	}
	switch def.Type {
	case "bool":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be true or false", def.Key)
		}
	case "enum":
		s, _ := value.(string)
		for _, o := range def.Options {
			if o == s {
				return nil
			}
		}
		return fmt.Errorf("%s must be one of %v", def.Key, def.Options)
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", def.Key)
		}
	}
	return nil
}

// --- the few TOML lines we touch ---
