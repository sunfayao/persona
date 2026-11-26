package extmgr

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/connector"
)

// keepStore is memStore with settings and connector secrets kept in memory.
type keepStore struct {
	*memStore
	mu       sync.Mutex
	settings map[string]string
	secrets  map[string]string
}

func newKeepStore() *keepStore {
	return &keepStore{memStore: newMemStore(), settings: map[string]string{}, secrets: map[string]string{}}
}

func (s *keepStore) Setting(_ context.Context, key string, out any) (bool, error) {
	s.mu.Lock()
	raw, ok := s.settings[key]
	s.mu.Unlock()
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal([]byte(raw), out)
}

func (s *keepStore) PutSetting(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	s.mu.Lock()
	s.settings[key] = string(raw)
	s.mu.Unlock()
	return err
}

func (s *keepStore) ConnectorSecret(_ context.Context, id string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.secrets[id]
	return v, ok, nil
}

func (s *keepStore) PutConnectorSecret(_ context.Context, id, sealed string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets[id] = sealed
	return nil
}

func (s *keepStore) DeleteConnectorSecret(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, id)
	return nil
}

// signInConnector signs in by phone, code and (made-up) 2FA password, keeping its session in the hub's secrets.
type signInConnector struct {
	exampleConnector
	secrets connector.Secrets
	phone   string
	out     bool
}

func (*signInConnector) ID() string   { return "example" }
func (*signInConnector) Name() string { return "Example" }
func (*signInConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapTools, connector.CapActions,
		connector.CapLogin}}
}

func (c *signInConnector) LoginStep(ctx context.Context, step string, v map[string]string) (connector.Prompt, error) {
	phone := connector.Prompt{Step: "phone", Fields: []connector.Field{{Name: "phone", Label: "手机号", Placeholder: "+1 555 0100"}}}
	code := connector.Prompt{Step: "code", Fields: []connector.Field{{Name: "code", Label: "验证码"}}, Hint: "验证码发到了你别的设备上"}
	password := connector.Prompt{Step: "password", Fields: []connector.Field{{Name: "password", Label: "两步验证密码", Secret: true}}}
	switch step {
	case "":
		return phone, nil
	case "phone":
		c.phone = v["phone"]
		return code, nil
	case "code":
		if v["code"] != "12345" {
			code.Error = "验证码不对"
			return code, nil
		}
		return password, nil
	case "password":
		if v["password"] != "example-password" {
			password.Error = "密码不对"
			return password, nil
		}
		if err := c.secrets.Save(ctx, []byte("session for "+c.phone)); err != nil {
			return connector.Prompt{}, err
		}
		return connector.Prompt{Done: true, Account: "示例用户"}, nil
	}
	return phone, nil
}

func (c *signInConnector) Logout(ctx context.Context) error {
	c.out = true
	return c.secrets.Delete(ctx)
}

// forgettingAssistant is a fake assistant reporting the inbox deletion count.
func forgettingAssistant(h *Manager) *fakeAssistant {
	a := withAssistant(h)
	a.on(assistantwire.MethodInboxForget, func(json.RawMessage) (any, error) { return map[string]any{"deleted": 1}, nil })
	return a
}
