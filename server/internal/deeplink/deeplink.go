// Package deeplink builds the links the hub writes to Persona's pages: the paths of the route table in
// docs/protocol/deep-links.md (#9926), and their two forms. The web link is <public_url><path>, what the hub sends out
// (Lark cards take only http(s), a browser opens it where Persona is not installed); every app opens its own hub's web
// links natively. The app link is persona://open<path>, for the apps themselves.
//
// Path segments are escaped as url.PathEscape does and query values as url.QueryEscape; the query keys follow the
// table's order. spec/fixtures/deep-links/cases.json holds what each builder writes, byte for byte.
package deeplink

import (
	"net/url"
	"strings"
)

// AppPrefix is what an app link has in place of the hub's address.
const AppPrefix = "persona://open"

// Web is path's link on the hub's web app at base (its public_url; a trailing "/" is dropped, a sub-path kept); ""
// when base is, so a hub with no public address writes no link.
func Web(base, path string) string {
	base = strings.TrimRight(base, "/")
	if base == "" || path == "" {
		return ""
	}
	return base + path
}

// Conversation is one of the assistant's conversations.
func Conversation(id string) string { return "/persona/" + seg(id) }

// Session is an Agent's conversation.
func Session(machine, session string) string { return "/s/" + seg(machine) + "/" + seg(session) }

// At is path at one message (item, the message's id): the page lands on it. path unchanged when item is "".
func At(path, item string) string { return query(path, "at", item) }

// Todo is one to-do. It is a path: the older /todos?id=<id> is still read by every client, never written.
func Todo(id string) string { return "/todos/" + seg(id) }

// settingsItems is the query key that names an item of a settings tab, for the tabs that have one
// (web/src/views/settings/nav.ts).
var settingsItems = map[string]string{"models": "purpose", "channels": "channel", "extensions": "extension", "connectors": "connector"}

// Settings is Settings, at tab when it is not "", at that tab's item when the tab has one and item is not "".
func Settings(tab, item string) string {
	if tab == "" {
		return "/settings"
	}
	key := settingsItems[tab]
	if key == "" {
		item = ""
	}
	return query("/settings", "tab", tab, key, item)
}

// seg is one path segment.
func seg(s string) string { return url.PathEscape(s) }

// query adds key/value pairs to path in their order, leaving out empty values, after any query path already has.
func query(path string, kv ...string) string {
	var b strings.Builder
	b.WriteString(path)
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i] == "" || kv[i+1] == "" {
			continue
		}
		b.WriteString(sep)
		b.WriteString(kv[i])
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(kv[i+1]))
		sep = "&"
	}
	return b.String()
}
