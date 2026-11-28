package traex

import "testing"

func TestParseCallUsesNativeArguments(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		record string
		name   string
		input  string
	}{
		{`{"type":"function_call","call_id":"native-id","name":"Read","arguments":"{\"file_path\":\"example.txt\"}"}`, "Read", `{"file_path":"example.txt"}`},
		{`{"type":"function_call","call_id":"native-id","name":"Grep","arguments":{"pattern":"needle"}}`, "Grep", `{"pattern":"needle"}`},
		{`{"type":"function_call_output","call_id":"native-id","name":"Grep","arguments":{}}`, "", ""},
		{`{"type":"function_call","call_id":"native-id","name":"Bash","arguments":{}}`, "", ""},
		{`{"type":"function_call","call_id":"native-id","name":"Read","arguments":"not JSON"}`, "", ""},
		{`{"type":"function_call","name":"Read","arguments":{}}`, "", ""},
	} {
		call, ok := (RolloutTools{}).Call([]byte(tc.record))
		if ok != (tc.name != "") || call.Name != tc.name || string(call.Input) != tc.input || ok && call.ID != "native-id" {
			t.Errorf("ParseCall(%s) = %+v, %t", tc.record, call, ok)
		}
	}
}
