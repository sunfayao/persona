package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/tooluse"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// The assistant's conversations' tool calls are recorded (assistant/tooluse): how often each tool is called on each
// runtime, and the friction, with no argument value kept. mcpCallWithOrigin records every call of an assistant
// conversation; tool() adds the code of a failure that only its error says (callRecord).

// newToolUse makes the record, on the assistant's store.
func (s *Service) newToolUse() *tooluse.Book {
	return tooluse.New(tooluse.Host{
		Load: func(ctx context.Context, key string, out any) (bool, error) {
			st := s.store()
			if st == nil {
				return false, nil
			}
			return st.Setting(ctx, key, out)
		},
		Save: func(ctx context.Context, key string, v any) error {
			st := s.store()
			if st == nil {
				return nil
			}
			return st.PutSetting(ctx, key, v)
		},
		Now:  time.Now,
		Logf: s.logf,
	})
}

// toolUseFlushEvery is how often what the record gained is stored.
const toolUseFlushEvery = 5 * time.Minute

// toolUseLoop loads the record, stores what it gains every toolUseFlushEvery, and once more when the manager stops.
func (s *Service) toolUseLoop(ctx context.Context) {
	s.toolUse.Load(ctx)
	ticker := time.NewTicker(toolUseFlushEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			last, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			if err := s.toolUse.Flush(last); err != nil {
				s.logf("tool use: save: %v", err)
			}
			cancel()
			return
		case <-ticker.C:
			if err := s.toolUse.Flush(ctx); err != nil {
				s.logf("tool use: save: %v", err)
			}
			s.toolNotes.Sweep(ctx) // the field notes' review dates, stale notes and covered fixes (tool_notes.go)
		}
	}
}

// callRecord is what tool() learns of a call that the result alone does not say: the code of its error.
type callRecord struct{ code string }

type callRecordKey struct{}

func withCallRecord(ctx context.Context, r *callRecord) context.Context {
	return context.WithValue(ctx, callRecordKey{}, r)
}

// noteCallError keeps err's code for the call being recorded, if one is: an RPC error's code, else the message key
// of an error built with i18n.Err.
func noteCallError(ctx context.Context, err error) {
	r, _ := ctx.Value(callRecordKey{}).(*callRecord)
	if r == nil || err == nil || r.code != "" {
		return
	}
	var coded *rpc.Error
	var keyed *i18n.Message
	switch {
	case errors.As(err, &coded) && coded.Code != "":
		r.code = coded.Code
	case errors.As(err, &keyed) && keyed.Key != "":
		r.code = keyed.Key
	}
}

// noteCallCode keeps code for the call being recorded, if one is and it has none yet: a refusal answered in text only.
func noteCallCode(ctx context.Context, code string) {
	if r, _ := ctx.Value(callRecordKey{}).(*callRecord); r != nil && r.code == "" {
		r.code = code
	}
}

// resultCode is the code of a failed result that noteCallError did not see (an extension's tool, a refusal before the
// tool ran): the "code" of a JSON answer (Cua Driver's invalid_batch), else what its text starts with.
func resultCode(res proto.ToolResult) string {
	var text strings.Builder
	for _, c := range res.Content {
		text.WriteString(c.Text)
	}
	t := strings.TrimSpace(text.String())
	var coded struct {
		Code string `json:"code"`
	}
	switch {
	case strings.HasPrefix(t, "{") && json.Unmarshal([]byte(t), &coded) == nil && coded.Code != "":
		return coded.Code
	case strings.HasPrefix(t, "unknown tool "):
		return "unknown_tool"
	case strings.HasPrefix(t, "invalid arguments"):
		return "invalid_arguments"
	case res.ErrorKind != "":
		return res.ErrorKind
	}
	return "error"
}

// recordToolCall keeps one call of an assistant conversation.
func (s *Service) recordToolCall(ctx context.Context, machine, session, name string, args json.RawMessage, r *callRecord, res proto.ToolResult) {
	if s.toolUse == nil {
		return
	}
	c := tooluse.Call{Tool: name, Runtime: s.callerRuntime(ctx), Machine: s.canonical(machine), Session: session,
		Skeleton: tooluse.Skeleton(args), OK: !res.IsError, Kind: res.ErrorKind}
	if res.IsError {
		c.Code = cmp.Or(r.code, resultCode(res))
	}
	s.toolUse.Record(c)
}

// runtimeCache holds callerRuntime's answer for a minute: it is asked on every call.
type runtimeCache struct {
	mu      sync.Mutex
	runtime string
	at      time.Time
}

// callerRuntime is the runtime the assistant's conversations run on, for the tool record: the main chat's launcher's
// kind (claude, codex, agy…), as AGENTS.md is worded for it (agentsRuntime). A side conversation on another launcher
// counts as the main chat's; the node does not say which runtime calls.
func (s *Service) callerRuntime(ctx context.Context) string {
	s.runtimeOfCalls.mu.Lock()
	defer s.runtimeOfCalls.mu.Unlock()
	if !s.runtimeOfCalls.at.IsZero() && time.Since(s.runtimeOfCalls.at) < time.Minute {
		return s.runtimeOfCalls.runtime
	}
	settings := s.settings(ctx)
	launcher := cmp.Or(s.mainLauncherNow(ctx), settings.Launcher)
	kind := assistantKind(launcher)
	if kind == "" && launcher != "" {
		kind = s.launcherKindOn(ctx, settings.MachineID, launcher)
	}
	s.runtimeOfCalls.runtime, s.runtimeOfCalls.at = kind, time.Now()
	return kind
}
