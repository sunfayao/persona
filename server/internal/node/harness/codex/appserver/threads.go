package appserver

import (
	"context"
	"encoding/json"
	"errors"
)

func (c *Wire) LoadedThreads(ctx context.Context) ([]string, error) {
	var ids []string
	cursor := ""
	seen := map[string]bool{}
	for {
		var page struct {
			Data       []string `json:"data"`
			NextCursor *string  `json:"nextCursor"`
		}
		p := map[string]any{"limit": 100}
		if cursor != "" {
			p["cursor"] = cursor
		}
		if err := c.Call(ctx, "thread/loaded/list", p, &page); err != nil {
			return nil, err
		}
		ids = append(ids, page.Data...)
		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		cursor = *page.NextCursor
		if seen[cursor] {
			return nil, errors.New("app-server 返回重复的分页游标")
		}
		seen[cursor] = true
	}
	return ids, nil
}

type Thread struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Model          string          `json:"model"`
	Effort         string          `json:"reasoningEffort"` // configured now; null (unset) reads as ""
	Preview        string          `json:"preview"`
	Cwd            string          `json:"cwd"`
	Path           string          `json:"path"`
	ParentThreadID string          `json:"parentThreadId"`
	Ephemeral      bool            `json:"ephemeral"`
	Source         json.RawMessage `json:"source"`
	Status         struct {
		Type        string   `json:"type"`
		ActiveFlags []string `json:"activeFlags"`
	} `json:"status"`
	Turns []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"turns"`
}
