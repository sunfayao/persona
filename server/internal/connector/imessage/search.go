package imessage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/lazyre"
)

// Keep modes: which conversations go to Persona.
const (
	// KeepContacts: one where someone is in this Mac's contacts, or the user has written: what a person would call a
	// conversation, without the codes, banks and parcels that come from numbers nobody saved.
	KeepContacts = "contacts"
	KeepAll      = "all"
)

// ErrTooManyAssociations means a read would exceed its bounded expansion of message-to-chat associations.
var ErrTooManyAssociations = errors.New("too many message-to-chat associations")

type conversationMessage struct {
	conversation string
	rowID        int64
}

func anyNamed(people []Person) bool {
	for _, p := range people {
		if p.Name != "" {
			return true
		}
	}
	return false
}

// wrote says which conversations the user has written in, ever.
func (r *Reader) wrote(ctx context.Context, convs []*conversation) (map[string]bool, error) {
	var chats []int64
	owner := map[int64]string{}
	for _, cv := range convs {
		for _, id := range cv.chats {
			chats = append(chats, id)
			owner[id] = cv.id
		}
	}
	var rows []struct {
		Chat int64 `json:"chat"`
	}
	if err := r.query(ctx, r.chatDB(), `SELECT DISTINCT j.chat_id AS chat FROM chat_message_join j JOIN message m ON m.ROWID = j.message_id
WHERE j.chat_id IN (`+ints(chats)+`) AND m.is_from_me = 1`, &rows); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, row := range rows {
		out[owner[row.Chat]] = true
	}
	return out, nil
}

// appleNanos is t as Messages dates it: nanoseconds since 2001.
func appleNanos(t time.Time) int64 { return (t.Unix() - appleEpoch) * 1e9 }

var (
	codeWords = lazyre.New(`(?i)验证码|校验码|动态码|确认码|动态密码|安全码|激活码|\bcode\b|\botp\b|passcode|\bpin\b`) // i18n-ignore: matches verification code labels in received messages
	codeDigit = lazyre.New(`\b\d{4,8}\b`)
)

// MaskCodes hides the digits of a message that is about a code (verification code 123456 → verification code ••••): what Persona keeps
// and gives to models does not carry one-time codes.
func MaskCodes(s string) string {
	if !codeWords.MatchString(s) {
		return s
	}
	return codeDigit.ReplaceAllString(s, "••••")
}

// Hit is a message a search found, with its conversation.
type Hit struct {
	Chat    Chat    `json:"chat"`
	Message Message `json:"message"`
}

// searchScan is how many distinct newest messages a search reads: the texts only in attributedBody have to be
// decoded to be matched, so the matching is here, not in SQL. Expansion to chat associations is bounded separately.
const (
	searchScan            = 20000
	maxSearchAssociations = 40000
)

// Search finds the newest messages since then whose text contains query (any case), in the conversations keep lets
// through, at most limit, codes masked.
func (r *Reader) Search(ctx context.Context, query string, limit int, since time.Time, keep string) ([]Hit, error) {
	return r.search(ctx, query, limit, since, keep, searchScan)
}

func (r *Reader) search(ctx context.Context, query string, limit int, since time.Time, keep string, scan int) ([]Hit, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, fmt.Errorf("empty query")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var rows []msgRow
	if err := r.query(ctx, r.chatDB(), fmt.Sprintf(`WITH selected AS (
  SELECT m.ROWID AS id FROM message m
  WHERE m.date >= %d AND m.associated_message_type = 0 AND m.item_type = 0
    AND EXISTS (SELECT 1 FROM chat_message_join j WHERE j.message_id = m.ROWID)
  ORDER BY m.date DESC, m.ROWID DESC LIMIT %d
)
SELECT j.chat_id AS chat, %s FROM selected s JOIN message m ON m.ROWID = s.id
JOIN chat_message_join j ON j.message_id = m.ROWID
ORDER BY m.date DESC, m.ROWID DESC, j.chat_id LIMIT %d`, appleNanos(since), scan, msgColumns, maxSearchAssociations+1), &rows); err != nil {
		return nil, err
	}
	if len(rows) > maxSearchAssociations {
		return nil, ErrTooManyAssociations
	}
	var found []msgRow
	for _, m := range rows {
		if text, ok := m.text(); ok && strings.Contains(strings.ToLower(text), q) {
			found = append(found, m)
		}
	}
	if len(found) == 0 {
		return []Hit{}, nil
	}
	convs, err := r.conversations(ctx)
	if err != nil {
		return nil, err
	}
	byChat := map[int64]*conversation{}
	for _, cv := range convs {
		for _, id := range cv.chats {
			byChat[id] = cv
		}
	}
	var hitConvs []*conversation
	seen := map[string]bool{}
	seenMessages := map[conversationMessage]bool{}
	unique := found[:0]
	var ids []int64
	for _, m := range found {
		cv := byChat[m.ChatRow]
		if cv == nil || seenMessages[conversationMessage{cv.id, m.RowID}] {
			continue
		}
		seenMessages[conversationMessage{cv.id, m.RowID}] = true
		unique = append(unique, m)
		if !seen[cv.id] {
			seen[cv.id] = true
			hitConvs = append(hitConvs, cv)
		}
		ids = append(ids, m.Handle)
	}
	found = unique
	people, err := r.members(ctx, hitConvs)
	if err != nil {
		return nil, err
	}
	names := r.names(ctx)
	wrote := map[string]bool{}
	if keep != KeepAll {
		if wrote, err = r.wrote(ctx, hitConvs); err != nil {
			return nil, err
		}
	}
	handles, err := r.handles(ctx, ids)
	if err != nil {
		return nil, err
	}
	chats := map[string]Chat{}
	for _, cv := range hitConvs {
		chats[cv.id] = r.chat(cv, people[cv.id], names)
	}
	hits := []Hit{}
	for _, m := range found {
		cv := byChat[m.ChatRow]
		if cv == nil || keep != KeepAll && !wrote[cv.id] && !anyNamed(chats[cv.id].Members) {
			continue
		}
		msg := message(m, names, handles, nil)
		msg.Text = MaskCodes(msg.Text)
		hits = append(hits, Hit{Chat: chats[cv.id], Message: msg})
		if len(hits) == limit {
			break
		}
	}
	return hits, nil
}
