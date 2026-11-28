package closeout

import (
	"strings"

	"github.com/sunfayao/persona/server/internal/textfmt"
)

const reportForm = `

- Status: done|not_done|blocked
- Merged: yes|no|n/a
- Deployed: yes|no|n/a
- Did: …
- Artifact: pr|commit|branch|doc|build|url|file …
- Delivery: live|pr|merged|off|none
- Unblock: user|agent|other
- Verified: …
- Missing: …
- Waiting: …

---`

// writeForm is reportForm without Delivery and Unblock: the form of a turn no delivery standard applies to.
const writeForm = `

- Status: done|not_done|blocked
- Merged: yes|no|n/a
- Deployed: yes|no|n/a
- Did: …
- Artifact: pr|commit|branch|doc|build|url|file …
- Verified: …
- Missing: …
- Waiting: …

---`

// readonlyForm is the form of a read-only task: the three fields every report starts with, what it found, the file it
// made for the user and how it checked. FinalWorkReport reads it as any other.
const readonlyForm = `

- Status: done|not_done|blocked
- Merged: n/a
- Deployed: n/a
- Did: …
- Artifact: file …
- Verified: …

---`

// formStart is how every form begins after its heading, the read-only one too.
const formStart = "\n\n- Status: done|not_done|blocked\n"

// HasMarkdownInstructions identifies a form already supplied with the agent's task, whichever of the two it is.
func HasMarkdownInstructions(instructions string) bool {
	for _, heading := range []string{textfmt.WorkReportEnglish, textfmt.WorkReportChinese, textfmt.WorkReportJapanese} {
		if strings.Contains(instructions, heading+formStart) {
			return true
		}
	}
	return false
}

// MarkdownExample is the terminal-readable form supplied to a node that announced version 1, with the delivery fields.
func MarkdownExample(language string) string { return reportHeading(language) + reportForm }

// MarkdownWrite is MarkdownExample for work no delivery standard applies to: no Delivery and Unblock lines.
func MarkdownWrite(language string) string { return reportHeading(language) + writeForm }

// MarkdownReadonly is MarkdownExample for a read-only task: nothing it does is merged, deployed or delivered.
func MarkdownReadonly(language string) string { return reportHeading(language) + readonlyForm }

func reportHeading(language string) string {
	switch language {
	case "日本語", "ja":
		return textfmt.WorkReportJapanese
	case "", "简体中文", "zh-Hans":
		return textfmt.WorkReportChinese
	default:
		return textfmt.WorkReportEnglish
	}
}
