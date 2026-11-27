package mail

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type ordinalAttachment struct {
	index    int
	filename string
	data     string
}

// Each bit makes one physical attachment undecodable. The last two have identical
// names and bytes, so a reader must use the ordinal rather than a filename fallback.
func attachmentOrdinalMessage(bad int, nested bool) (string, []ordinalAttachment) {
	raw := "From: Sender <sender@example.com>\r\nTo: recipient@example.com\r\n" +
		"Cc: copy@example.com\r\nSubject: MIME order\r\nMessage-ID: <order@example.com>\r\n" +
		"In-Reply-To: <parent@example.com>\r\nReferences: <root@example.com> <parent@example.com>\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=outer\r\n\r\n" +
		"--outer\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nvalid body\r\n" +
		"--outer\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>valid body</p>\r\n" +
		"--outer\r\nContent-Type: image/png\r\nContent-Disposition: inline; filename=inline.png\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\nYWJj\r\n" +
		"--outer\r\nContent-Type: text/plain; name=notes.txt\r\n\r\nnon-attachment text\r\n"
	boundary := "outer"
	if nested {
		raw += "--outer\r\nContent-Type: multipart/mixed; boundary=inner\r\n\r\n"
		boundary = "inner"
	}
	var want []ordinalAttachment
	for i, part := range []struct{ filename, encoded, data string }{
		{"", "eHl6", "xyz"},
		{"same.pdf", "YWJj", "abc"},
		{"same.pdf", "YWJj", "abc"},
	} {
		raw += "--" + boundary + "\r\nContent-Type: application/pdf\r\n"
		if i > 0 {
			raw += "Content-Disposition: attachment; filename=" + part.filename + "\r\n"
		} // An unnamed non-text part is also an AttachmentHeader.
		encoded := part.encoded
		if bad&(1<<i) != 0 {
			// Includes a decoded prefix before failure, which must not be listed.
			encoded = "YWJj!!!!"
		} else {
			want = append(want, ordinalAttachment{i + 1, part.filename, part.data})
		}
		raw += "Content-Transfer-Encoding: base64\r\n\r\n" + encoded + "\r\n"
	}
	if nested {
		raw += "--inner--\r\n"
	}
	// Explicit text attachments count, unlike inline text or inline images.
	raw += "--outer\r\nContent-Type: text/csv\r\nContent-Disposition: attachment; filename=table.csv\r\n\r\na,b\r\n--outer--\r\n"
	want = append(want, ordinalAttachment{4, "table.csv", "a,b"})
	return raw, want
}

func checkOrdinalAttachments(t *testing.T, msg *MailMessage, want []ordinalAttachment) {
	t.Helper()
	if msg.Text != "valid body" || msg.HTML != "<p>valid body</p>" || msg.InReplyTo != "parent@example.com" ||
		!reflect.DeepEqual(msg.References, []string{"root@example.com", "parent@example.com"}) ||
		!reflect.DeepEqual(msg.To, []Address{{Email: "recipient@example.com"}}) ||
		!reflect.DeepEqual(msg.Cc, []Address{{Email: "copy@example.com"}}) {
		t.Fatalf("body or headers changed: %+v", msg)
	}
	if len(msg.Attachments) != len(want) {
		t.Fatalf("attachments = %+v, want %d decodable parts", msg.Attachments, len(want))
	}
	for i, expected := range want {
		a := msg.Attachments[i]
		mime := "application/pdf"
		if expected.index == 4 {
			mime = "text/csv"
		}
		if !strings.HasPrefix(a.ID, strconv.Itoa(expected.index)+"-") || a.Filename != expected.filename ||
			a.Mime != mime || a.Size != int64(len(expected.data)) {
			t.Errorf("attachment = %+v, want physical part %+v (%s)", a, expected, mime)
		}
	}
}

func TestParseMessageAttachmentMIMEOrdinals(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for bad := 0; bad < 8; bad++ {
			t.Run(fmt.Sprintf("nested=%t/bad=%03b", nested, bad), func(t *testing.T) {
				raw, want := attachmentOrdinalMessage(bad, nested)
				msg := &MailMessage{MailSummary: MailSummary{AccountID: "account", Mailbox: "Archive", UID: 7, Seen: true, Flagged: true}}
				parseMessage([]byte(raw), msg)
				checkOrdinalAttachments(t, msg, want)
				if msg.AccountID != "account" || msg.Mailbox != "Archive" || msg.UID != 7 || !msg.Seen || !msg.Flagged {
					t.Fatalf("summary changed: %+v", msg.MailSummary)
				}
			})
		}
	}
}
