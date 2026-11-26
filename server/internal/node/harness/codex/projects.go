package codex

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/nwverse/persona/server/internal/pathutil"
)

// Native project metadata is read to place new desktop chats in an existing
// project. Persona does not write the desktop project's database or sidebar state.
type DesktopProject struct {
	ID       string               `json:"id"`
	Name     string               `json:"name"`
	Roots    []DesktopProjectRoot `json:"roots"`
	Position int64                `json:"position"`
}

type DesktopProjectRoot struct {
	Path string `json:"path"`
}

// listCodexDesktopProjects reads every page in the app-server's position order.
// A broken/repeated cursor is an error rather than an infinite startup loop.
func ListDesktopProjects(call func(string, any, any) error) ([]DesktopProject, error) {
	var projects []DesktopProject
	var cursor *string
	seenCursors := map[string]bool{}
	seenIDs := map[string]bool{}
	for {
		var response struct {
			Data       []DesktopProject `json:"data"`
			NextCursor *string          `json:"nextCursor"`
		}
		if err := call("project/list", map[string]any{"cursor": cursor, "limit": 100, "sortKey": "position"}, &response); err != nil {
			return nil, err
		}
		for _, project := range response.Data {
			if strings.TrimSpace(project.ID) == "" {
				return nil, errors.New("project/list: project has no id")
			}
			if seenIDs[project.ID] {
				continue
			}
			seenIDs[project.ID] = true
			projects = append(projects, project)
		}
		if response.NextCursor == nil {
			return projects, nil
		}
		if len(seenCursors) >= 100 || *response.NextCursor == "" || seenCursors[*response.NextCursor] {
			return nil, errors.New("project/list: invalid or repeated pagination cursor")
		}
		seenCursors[*response.NextCursor] = true
		cursor = response.NextCursor
	}
}

// codexDesktopProjectIDForDir selects the most specific registered root. Windows
// spelling differences and symlinks are normalized as for the desktop catalog;
// sibling names with a shared prefix do not match. No project is created here.
func desktopProjectIDForDir(projects []DesktopProject, cwd string) string {
	if !filepath.IsAbs(cwd) {
		return ""
	}
	cwd = pathutil.FolderKey(pathutil.Real(filepath.Clean(cwd)))
	var best DesktopProject
	bestRoot := ""
	for _, project := range projects {
		if project.ID == "" {
			continue
		}
		for _, root := range project.Roots {
			if !filepath.IsAbs(root.Path) {
				continue
			}
			path := pathutil.FolderKey(pathutil.Real(filepath.Clean(root.Path)))
			if !pathutil.WithinFolder(cwd, path) {
				continue
			}
			if len(path) > len(bestRoot) || len(path) == len(bestRoot) &&
				(project.Position < best.Position || project.Position == best.Position && project.ID < best.ID) {
				best, bestRoot = project, path
			}
		}
	}
	return best.ID
}

// selectCodexDesktopProject never substitutes another project for an explicit
// selection, even if multiple projects share a directory. Live API roots are
// checked immediately before thread/start so stale cards cannot misassign chats.
func SelectDesktopProject(projects []DesktopProject, cwd, projectID string) (string, error) {
	if projectID == "" {
		return desktopProjectIDForDir(projects, cwd), nil
	}
	for _, project := range projects {
		if project.ID != projectID {
			continue
		}
		if desktopProjectIDForDir([]DesktopProject{project}, cwd) == projectID {
			return projectID, nil
		}
		return "", errors.New("the selected directory is outside the selected Codex project's roots")
	}
	return "", errors.New("the selected Codex project is no longer available from the desktop project API")
}
