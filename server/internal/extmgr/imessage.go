package extmgr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/connector/imessage"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Messages (connector/imessage): the app reads one Mac's Messages through that Mac's node;
// the hub passes the pages along and keeps none of them. With "Hand to Persona" on (the user's switch, on by default,
// imessageAIKey) the real conversations also go to the inbox and the assistant's tools
// (connector/imessage/persona.go). No number or message is logged (only the connector, the Mac and the kind of failure).

// imessageContextLock is a lock a settings request can stop waiting for when its context ends.
type imessageContextLock struct {
	once  sync.Once
	token chan struct{}
}

func (l *imessageContextLock) lock(ctx context.Context) error {
	l.once.Do(func() {
		l.token = make(chan struct{}, 1)
		l.token <- struct{}{}
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.token:
		if err := ctx.Err(); err != nil {
			l.unlock()
			return err
		}
		return nil
	}
}

func (l *imessageContextLock) unlock() { l.token <- struct{}{} }

// imessageAIKey is the settings key of "Hand to Persona": absent means on.
const imessageAIKey = "imessage.ai"

func (h *Manager) addIMessage(cfg imessage.Config) error {
	name := cfg.Machine
	if m := h.localMachine(cfg.Machine); m != nil {
		name = m.name()
	}
	c := imessage.NewRemote(cfg, name, h.imessageCall(cfg.Machine))
	c.Zone = h.userLocation
	on, err := h.imessageAI(context.Background())
	if err != nil {
		on = false
		h.imessageAIPending.Store(true)
	}
	c.SetAI(on)
	return h.addConnector(c)
}

// imessageAI is the user's "Hand to Persona" choice: on unless they turned it off.
func (h *Manager) imessageAI(ctx context.Context) (bool, error) {
	if h.store == nil {
		return true, nil
	}
	var on bool
	ok, err := h.store.Setting(ctx, imessageAIKey, &on)
	if err != nil {
		return false, err
	}
	return !ok || on, nil
}

// A failed startup read or an uncertain settings write stays off until the
// persisted choice can be read again. Serialize recovery with a user's PUT.
func (h *Manager) reconcileIMessageAI(ctx context.Context) {
	if !h.imessageAIPending.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := h.imessageSettingsMu.lock(ctx); err != nil {
		return
	}
	defer h.imessageSettingsMu.unlock()
	if !h.imessageAIPending.Load() {
		return
	}
	c := h.imessage()
	if c == nil {
		return
	}
	on, err := h.imessageAI(ctx)
	if err != nil {
		return
	}
	c.SetAI(on)
	h.imessageAIPending.Store(false)
	if on {
		h.kickInbox()
	}
}

var errNotAMachine = errors.New("信息's Mac is not a machine of this hub")

// imessageCall asks the Mac's node, turning its failures into the connector's errors.
func (h *Manager) imessageCall(machine string) imessage.Caller {
	return func(ctx context.Context, method string, params, out any) error {
		m := h.localMachine(machine)
		if m == nil {
			return errNotAMachine
		}
		err := m.callBuiltinProgram(ctx, imessageExtensionID, method, params, out)
		var re *rpc.Error
		switch {
		case err == nil:
			return nil
		case errors.Is(err, errOffline):
			return imessage.ErrOffline
		case errors.As(err, &re) && strings.HasPrefix(re.Message, "unknown method"):
			return imessage.ErrNodeTooOld
		case errors.As(err, &re) && re.Code == proto.CodeNoPermission:
			return imessage.ErrNoPermission
		case errors.As(err, &re) && re.Code == proto.CodeNoChat:
			return imessage.ErrNoChat
		}
		return fmt.Errorf("%s: %w", method, err)
	}
}

func (h *Manager) imessage() *imessage.Remote {
	c, _ := h.connectors.Get(imessage.ID).(*imessage.Remote)
	return c
}

func (h *Manager) imessageRoutes(api *http.ServeMux) {
	// DELETE /api/v1/imessage/data: what the hub kept of iMessage (its inbox items),
	// once "Hand to Persona" is off — on, the next sync would bring it back.
	api.HandleFunc("DELETE /api/v1/imessage/data", func(w http.ResponseWriter, r *http.Request) {
		c := h.imessage()
		if c == nil {
			writeCode(w, http.StatusNotFound, "not_configured", i18n.T(h.requestLang(r), "hub-imessage.not_configured"), nil)
			return
		}
		if c.AI() {
			writeCode(w, http.StatusConflict, "ai_on", i18n.T(h.requestLang(r), "hub-imessage.ai_on"), nil)
			return
		}
		res, err := h.forgetIMessage(r.Context())
		if err != nil {
			h.logf("imessage: delete: %v", err)
			writeForgetErr(w, err)
			return
		}
		h.logf("imessage: deleted %d inbox item(s)", res.Inbox)
		writeJSON(w, res)
	})
}

// forgetIMessage removes what the hub kept of iMessage (forgetSource).
func (h *Manager) forgetIMessage(ctx context.Context) (sourceForgotten, error) {
	return h.forgetSource(ctx, imessage.ID)
}
