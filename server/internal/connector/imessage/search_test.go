package imessage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var fixtureSince = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

// Everything after a cursor, by ROWID: the real conversations only (someone in the contacts, or the user wrote),
// tapbacks left out; the cursor moves past what was left out too.
func TestSearch(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	since := time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC)
	hits, err := r.Search(ctx, "周五", 10, since, KeepContacts)
	if err != nil {
		t.Fatal(err)
	}
	// Only in attributedBody: decoded to be found. The tapback that quotes it is not a message.
	if len(hits) != 1 || hits[0].Chat.Name != "张三" || hits[0].Message.Text != "示例消息：周五下午三点见" {
		t.Fatalf("hits %+v", hits)
	}
	if hits, _ := r.Search(ctx, "验证码", 10, since, KeepContacts); len(hits) != 0 {
		t.Errorf("a short code's message found: %+v", hits)
	}
	hits, _ = r.Search(ctx, "验证码", 10, since, KeepAll)
	if len(hits) != 1 || hits[0].Message.Text != "【示例】验证码 ••••，5 分钟内有效" {
		t.Errorf("all: %+v", hits)
	}
	hits, _ = r.Search(ctx, "EXAMPLE.COM", 10, since, KeepContacts)
	if len(hits) != 1 || hits[0].Chat.Name != "Alice Example" {
		t.Errorf("any case: %+v", hits)
	}
	hits, _ = r.Search(ctx, "示例", 2, since, KeepContacts)
	if len(hits) != 2 || !hits[0].Message.At.After(hits[1].Message.At) {
		t.Errorf("newest first, two: %+v", hits)
	}
	if _, err := r.Search(ctx, " ", 10, since, KeepContacts); err == nil {
		t.Error("empty query")
	}
}

func TestMaskCodes(t *testing.T) {
	for in, want := range map[string]string{
		"【示例】验证码 123456，5 分钟内有效":            "【示例】验证码 ••••，5 分钟内有效",
		"Your code is 8841 for example.com": "Your code is •••• for example.com",
		"动态密码 00912345":                     "动态密码 ••••",
		"示例：明天下午 1530 在 2 号会议室":             "示例：明天下午 1530 在 2 号会议室", // no code word: as it is
		"PIN: 4321":      "PIN: ••••",
		"barcode 123456": "barcode 123456", // a word that only contains "code"
	} {
		if got := MaskCodes(in); got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
}

// The assistant and the inbox list and read only what goes to Persona; the app's own view lists everything.
func TestChatsAndMessagesKept(t *testing.T) {
	r := fixture(t)
	ctx := context.Background()
	all, err := r.Chats(ctx, 50, "", "")
	if err != nil {
		t.Fatal(err)
	}
	kept, err := r.Chats(ctx, 50, "", KeepContacts)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	code := ""
	for _, c := range kept.Chats {
		names = append(names, c.Name)
	}
	for _, c := range all.Chats {
		if c.Name == "12345" {
			code = c.ID
		}
	}
	if len(all.Chats) != 5 || strings.Join(names, "|") != "张三|李四、+15550102|周末徒步|Alice Example" {
		t.Fatalf("all %d, kept %v", len(all.Chats), names)
	}
	if _, err := r.Messages(ctx, code, 10, "", KeepContacts); !errors.Is(err, ErrNoChat) {
		t.Errorf("the short code's conversation for Persona: %v", err)
	}
	if p, err := r.Messages(ctx, code, 10, "", ""); err != nil || len(p.Messages) != 3 {
		t.Errorf("the short code's conversation in the app: %v", err)
	}
}
