package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/mail"
	"github.com/nwverse/persona/server/internal/proto"
)

// mailConnector is mail as a connector (docs/extensions/connectors.md "3. Migration"): its inbox, its tools and mail_send
// through the gate;
// Settings › Connectors draws its row from the connectors list (?all=1, migration M3–M6).
type mailConnector struct{ h *Manager }

func (mailConnector) ID() string   { return "mail" }
func (mailConnector) Name() string { return "邮件" }

// Manifest: the coding agents get mail too. Its inbox is the mail rounds' (readMail), with the Add to Inbox switch (#5307).
func (mailConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapTools, connector.CapActions},
		AgentTools: []string{"mail_*"}, InboxSwitch: true}
}

// Tools are mail's (prompts/mcp/mail) without mail_send, which is an action: the user allows each mail on the phone
// (the gate), the card as it always was.
func (c mailConnector) Tools() []proto.Tool {
	var out []proto.Tool
	for _, t := range c.h.mail.Tools() {
		if t.Name != mail.ToolSend {
			out = append(out, t)
		}
	}
	return out
}

func (c mailConnector) Actions() []proto.Tool {
	for _, t := range c.h.mail.Tools() {
		if t.Name == mail.ToolSend {
			return []proto.Tool{t}
		}
	}
	return nil
}

func (c mailConnector) Call(ctx context.Context, name string, args json.RawMessage) (string, error) {
	return toolText(c.h.mail.Call(ctx, name, args, nil))
}

var _ connector.ReadJSON = mailConnector{}

// CallJSON is connector.ReadJSON: mail_accounts and mail_search as JSON for subscriptions (mail.Service.CallJSON),
// mail_read as its text.
func (c mailConnector) CallJSON(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, bool, error) {
	return c.h.mail.CallJSON(ctx, name, args)
}

// Describe refuses a mail_send that could not be sent before the user is asked.
func (mailConnector) Describe(_ context.Context, name string, args json.RawMessage) (connector.Action, string, error) {
	summary, err := mail.SendSummary(args)
	return connector.Action{Name: name}, summary, err
}

// Do sends it (the gate let it through).
func (c mailConnector) Do(ctx context.Context, name string, args json.RawMessage) (string, error) {
	done, err := c.DoRef(ctx, name, args)
	return done.Text, err
}

var _ connector.RefActions = mailConnector{}

// DoRef is Do with the Message-ID written into the header of the mail handed to the transport, without the angle
// brackets (connector.RefActions); whether a provider keeps it as written has not been verified.
func (c mailConnector) DoRef(ctx context.Context, name string, args json.RawMessage) (connector.DoResult, error) {
	res, messageID := c.h.mail.CallRef(ctx, name, args, nil)
	text, err := toolText(res)
	return connector.DoResult{Text: text, MessageRef: messageID}, err
}

// errMailAttachmentUnavailable: mail_save_attachment is not offered to the caller (mail is off, not set up, or not
// given to it), so neither are the bytes.
var errMailAttachmentUnavailable = errors.New("mail is not available")

// MailAttachment answers a node's mail.attachment (proto.MethodMailAttachment) for mail_save_attachment: one
// attachment's bytes, at most proto.MaxMailAttachment, only while the mail connector offers the tool to that caller
// (assistant: an assistant conversation, which the assistant has checked; an agent gets it only when mail's tools go to
// the agents too). The machine and conversation checks of mcp.call are the assistant's (fleet_tools.go mailAttachment).
func (h *Manager) MailAttachment(ctx context.Context, req proto.MailAttachmentRequest) (proto.MailAttachment, error) {
	if !h.ready(ctx) || h.mail == nil || h.connectors == nil || !h.offersTool(ctx, mail.ToolSaveAttachment, req.Assistant) {
		return proto.MailAttachment{}, errMailAttachmentUnavailable
	}
	a, data, err := h.mail.FetchAttachment(ctx, req.AccountID, req.UID, req.Mailbox, req.AttachmentID, proto.MaxMailAttachment)
	if err != nil {
		return proto.MailAttachment{}, err
	}
	return proto.MailAttachment{Filename: a.Filename, Mime: a.Mime, Size: a.Size, Data: data}, nil
}

// offersTool says whether a connector that is ok lists the tool name for this caller: the agents' tools to everyone,
// the assistant-only ones to an assistant conversation (what AgentTools hands out).
func (h *Manager) offersTool(ctx context.Context, name string, assistant bool) bool {
	is := func(t proto.Tool) bool { return t.Name == name }
	return slices.ContainsFunc(h.connectors.ToolsFor(ctx, true), is) ||
		assistant && slices.ContainsFunc(h.connectors.ToolsFor(ctx, false), is)
}

// toolText is a tool result as text, its error as an error (connector.ReadTools / Actions return them that way).
func toolText(r proto.ToolResult) (string, error) {
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	if r.IsError {
		return "", errors.New(b.String())
	}
	return b.String(), nil
}
