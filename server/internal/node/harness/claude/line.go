// Package claude supplies Claude Code's JSONL records to the node transcript reader.
package claude

import "github.com/sunfayao/persona/server/internal/transcript"

// Reader decodes JSONL records before the transcript reader projects their shared fields.
type Reader struct{}

func (Reader) DecodeLine(line []byte, out *transcript.ClaudeLine) error {
	if err := transcript.DecodeClaudeLine(line, out); err != nil {
		return err
	}
	result := &out.ToolUseResult
	out.TaskEvidence.ShellID = result.BackgroundTaskID
	out.TaskEvidence.ShellTimedOut = result.TimedOutAfterMs != nil && *result.TimedOutAfterMs > 0
	out.TaskEvidence.MonitorID = result.TaskID
	out.TaskEvidence.ResumedAgent = result.ResumedAgentID
	return nil
}
