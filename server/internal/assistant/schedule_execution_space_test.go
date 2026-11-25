package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Use a node-shaped prepare response: each space is prepared under the selected computer's own home.
func spaceExecutionService(t *testing.T) (*Service, *fakeHub, *[]map[string]string) {
	t.Helper()
	f := spaceHub()
	f.devices[0].Online = true
	f.devices = append(f.devices, Device{ID: "remote", Name: "Other computer", Home: "/home/other", Online: true})
	f.sessions["remote"] = nil
	f.answerApp(AppPlugins, PluginsAssistant, func(json.RawMessage) (any, error) {
		return map[string]any{"instructions": "# Main-space plugin marker", "skills": nil}, nil
	})
	f.launcherList = []proto.Launcher{{Name: "claude", Agent: "claude"}, {Name: "codex", Agent: "codex"}}
	f.zshrc = map[string][]proto.Launcher{"remote": {{Name: "claude", Agent: "claude"}, {Name: "codex", Agent: "codex"}}}
	prepared := []map[string]string{}
	f.answerNode(DeviceAssistantPrepare, func(machine string, raw json.RawMessage) (any, error) {
		var p struct {
			Dir string `json:"dir"`
			MD  string `json:"claude_md"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		home := "/home/me"
		if machine == "remote" {
			home = "/home/other"
		}
		cwd := path.Join(home, p.Dir)
		prepared = append(prepared, map[string]string{"machine": machine, "dir": p.Dir, "md": p.MD, "cwd": cwd})
		return map[string]any{"cwd": cwd}, nil
	})
	s := newTestService(t, f)
	setSettings(t, s, func(v *Settings) { v.MachineID, v.Main = "server", "main-0000-0000" })
	return s, f, &prepared
}

func TestTaskExecutionSpaceRejectsDirectoryOnSave(t *testing.T) {
	s, f, _ := spaceExecutionService(t)
	setTasks(s, Task{ID: "health", Title: "Space check", Prompt: "Check the summary", Space: testSpace, Repeat: repeatDaily, Time: "09:00", Enabled: true})
	r := request(s, "PUT", "/api/v1/assistant/tasks/health", `{"title":"Changed","prompt":"Check the summary","repeat":"daily","time":"09:00","enabled":true,"cwd":"/work/project","model":"test-model","model_revision":0}`)
	t.Logf("save result: %d %s", r.Code, r.Body)
	if r.Code != http.StatusBadRequest {
		t.Fatalf("health directory edit accepted: %d %s", r.Code, r.Body)
	}
	task, _ := taskByID(s, "health")
	record, err := s.modelRecord(context.Background(), "task.health")
	if err != nil || task.Title != "Space check" || task.Cwd != "" || record.Revision != 0 || f.sent() != 0 {
		t.Fatalf("rejection changed configuration: %+v %+v %v", task, record, err)
	}
}

func TestTaskExecutionSpaceRejectsStoredDirectoryBeforeDispatch(t *testing.T) {
	for _, manual := range []bool{true, false} {
		name := "scheduled"
		if manual {
			name = "manual"
		}
		t.Run(name, func(t *testing.T) {
			s, f, prepared := spaceExecutionService(t)
			task := Task{ID: "health", Title: "Space check", Prompt: "Check the summary", Space: testSpace, Cwd: "/work/project", Repeat: repeatOnce, At: time.Now(), Enabled: true}
			setTasks(s, task)
			_, err := s.runTask(task, time.Now(), time.Now(), manual, false)
			t.Logf("run result: %v", err)
			if err == nil || f.sent() != 0 || len(*prepared) != 0 {
				t.Fatalf("stored health override reached execution: err=%v sends=%d prepared=%v", err, f.sent(), *prepared)
			}
		})
	}
}
