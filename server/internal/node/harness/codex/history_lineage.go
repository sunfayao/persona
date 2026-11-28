package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sunfayao/persona/server/internal/filemeta"
	"github.com/sunfayao/persona/server/internal/nodeclient"
	"github.com/sunfayao/persona/server/internal/transcript"
)

// Revert keeps the abandoned rows. A continuation's session_meta names the retained base and its exclusive
// rollout ordinal. Each physical segment has its own history-database key, the final UUID in its filename.
type codexHistorySegment struct {
	id  string
	end *int64
}

func rolloutStem(path string) string {
	base := filepath.Base(path)
	for _, ext := range rolloutExts {
		if strings.HasSuffix(base, ext) {
			return base[:len(base)-len(ext)]
		}
	}
	return base
}

func IsContinuation(path string) bool {
	name := rolloutStem(path)
	return len(name) > 37 && name[len(name)-37] == '_' && IsUUID(name[len(name)-36:])
}

func codexHistorySegments(home string, t AppThread) ([]codexHistorySegment, error) {
	if !t.Paginated || t.RolloutPath == "" {
		return []codexHistorySegment{{id: t.ID}}, nil
	}
	p := t.RolloutPath
	var out []codexHistorySegment
	seen := map[string]bool{}
	var end *int64
	for {
		name := rolloutStem(p)
		if len(name) < 36 {
			return nil, fmt.Errorf("invalid Codex history segment")
		}
		id := name[len(name)-36:]
		if !nodeclient.ValidSessionID(id) || seen[id] {
			return nil, fmt.Errorf("invalid Codex history lineage")
		}
		seen[id] = true
		out = append(out, codexHistorySegment{id: id, end: end})
		f, err := transcript.OpenStream(p)
		if err != nil {
			if len(out) == 1 && os.IsNotExist(err) {
				return []codexHistorySegment{{id: t.ID}}, nil
			}
			return nil, err
		}
		line, err := bufio.NewReader(f).ReadBytes('\n')
		f.Close()
		if err != nil {
			return nil, err
		}
		var header struct {
			Type    string `json:"type"`
			Payload struct {
				Base *struct {
					Thread string `json:"thread_id"`
					End    int64  `json:"end_ordinal_exclusive"`
				} `json:"history_base"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &header); err != nil || header.Type != "session_meta" {
			return nil, fmt.Errorf("invalid Codex segment header")
		}
		base := header.Payload.Base
		if base == nil {
			break
		}
		if !nodeclient.ValidSessionID(base.Thread) || base.End < 0 {
			return nil, fmt.Errorf("invalid Codex history base")
		}
		paths := GlobRolloutPaths(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*"+base.Thread+".jsonl"))
		archived := GlobRolloutPaths(filepath.Join(home, "archived_sessions", "rollout-*"+base.Thread+".jsonl"))
		paths = filemeta.NewestFirst(append(paths, archived...))
		if len(paths) == 0 {
			out = append(out, codexHistorySegment{id: base.Thread, end: &base.End})
			break
		}
		p, end = paths[0], &base.End
	}
	slices.Reverse(out)
	return out, nil
}

func HistoryTurnsSQL(home string, t AppThread) (string, error) {
	if !nodeclient.ValidSessionID(t.ID) {
		return "", fmt.Errorf("invalid Codex history thread id")
	}
	if !t.Paginated {
		return fmt.Sprintf("WITH history AS (SELECT *, rollout_ordinal AS history_ordinal FROM thread_turns WHERE thread_id = '%s') ", t.ID), nil
	}
	segments, err := codexHistorySegments(home, t)
	if err != nil {
		return "", err
	}
	var parts []string
	for i, segment := range segments {
		where := fmt.Sprintf("thread_id = '%s'", segment.id)
		if segment.end != nil {
			where += fmt.Sprintf(" AND rollout_ordinal < %d", *segment.end)
		}
		parts = append(parts, fmt.Sprintf("SELECT *, %d AS segment FROM thread_turns WHERE %s", i, where))
	}
	return "WITH segments AS (" + strings.Join(parts, " UNION ALL ") +
		"), history AS (SELECT *, ROW_NUMBER() OVER (ORDER BY segment, rollout_ordinal) - 1 AS history_ordinal FROM segments) ", nil
}
