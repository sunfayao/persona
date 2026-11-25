package proc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
)

// Handle answers a linked program's calls (connector.secrets.*, without the namespace), as fromProgram.
func (c *Conn) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	return c.fromProgram(ctx, method, params)
}

// Event takes a linked program's event (changed, without the namespace).
func (c *Conn) Event(event string, params json.RawMessage) { c.onEvent(event, params) }

// fromProgram answers the program's calls: its secret, and only its own (the hub bound Secrets to this connector;
// nothing the program sends picks another), .
func (c *Conn) fromProgram(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case serve.MGhToken:
		return c.ghToken(ctx, params)
	}
	if c.cfg.Secrets == nil {
		return nil, errors.New("no secrets store")
	}
	switch method {
	case serve.MSecretsLoad:
		data, err := c.cfg.Secrets.Load(ctx)
		return serve.SecretData{Data: data}, err
	case serve.MSecretsSave:
		var p serve.SecretData
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return struct{}{}, c.cfg.Secrets.Save(ctx, p.Data)
	case serve.MSecretsDelete:
		return struct{}{}, c.cfg.Secrets.Delete(ctx)
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

func (c *Conn) onEvent(event string, params json.RawMessage) {
	if event != serve.EChanged {
		return
	}
	var e serve.ChangedEvent
	if json.Unmarshal(params, &e) != nil {
		return
	}
	scope := connector.Scope(e.Scope)
	if !scope.Known() {
		c.cfg.Logf("connector %s: unknown scope %q changed", c.cfg.ID, e.Scope)
		return
	}
	c.mu.Lock()
	c.pending[scope] = true
	c.mu.Unlock()
	select {
	case c.events <- struct{}{}:
	default: // Watch is woken already and takes this scope too
	}
}

// Watch passes the program's changes on until ctx ends; the hub restarts it like any connector's.
func (c *Conn) Watch(ctx context.Context, changed func(connector.Scope)) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.events:
			c.mu.Lock()
			pending := c.pending
			c.pending = map[connector.Scope]bool{}
			c.mu.Unlock()
			if len(pending) == 0 { // the scopes went with the wake before, as onEvent's signal came after
				continue
			}
			c.kick(ctx) // what changed may be its status too (signed out elsewhere)
			for _, scope := range []connector.Scope{connector.ScopeInbox} {
				if pending[scope] {
					changed(scope)
				}
			}
		}
	}
}
