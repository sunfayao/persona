package proto

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sunfayao/persona/server/internal/jsonsubset"
)

func TestTurnModelSamplesRoundTrip(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	source, err := os.ReadFile(filepath.Join(root, "schema", "wire", "turn-model.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := jsonsubset.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	files, err := filepath.Glob(filepath.Join(root, "spec", "fixtures", "turn-model", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("turn model fixture files: %v, %v", files, err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct {
			Cases []struct {
				Name       string
				Definition string
				Sample     json.RawMessage
			}
		}
		if err := json.Unmarshal(data, &fixture); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, c := range fixture.Cases {
			t.Run(filepath.Base(file)+"/"+c.Name, func(t *testing.T) {
				t.Parallel()
				newValue := turnModelDefs[c.Definition]
				if newValue == nil {
					t.Fatalf("no generated type for %s", c.Definition)
				}
				v := newValue()
				d := json.NewDecoder(bytes.NewReader(c.Sample))
				d.DisallowUnknownFields()
				if err := d.Decode(v); err != nil {
					t.Fatal(err)
				}
				out, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				want, err := jsonsubset.Decode(c.Sample)
				if err != nil {
					t.Fatal(err)
				}
				got, err := jsonsubset.Decode(out)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("written back differently:\nwant %s\n got %s", c.Sample, out)
				}
				if problem := jsonsubset.ValidateAt(schema, defs[c.Definition].(map[string]any), got, "$", 0); problem != "" {
					t.Error(problem)
				}
			})
		}
	}
}

func TestTurnModelFacetsTolerateNewFieldsAndValues(t *testing.T) {
	t.Parallel()
	var item Item
	itemJSON := `{"kind":"notice","text":"Status changed","facets":{"future":{},"tool":{"kind":"future_tool","name":"Example"},"notice":{"code":"future_notice","severity":"info"}}}`
	if err := json.Unmarshal([]byte(itemJSON), &item); err != nil {
		t.Fatal(err)
	}
	wantItem := Item{Kind: KindNotice, Text: "Status changed", Facets: &ItemFacets{
		Tool:   &ToolFacet{Kind: "future_tool", Name: "Example"},
		Notice: &NoticeFacet{Code: "future_notice", Severity: NoticeSeverityInfo},
	}}
	if !reflect.DeepEqual(item, wantItem) {
		t.Errorf("decoded item = %#v, want %#v", item, wantItem)
	}
	var session Session
	if err := json.Unmarshal([]byte(`{"session_id":"s1","facets":{"future":{},"turn":{"open":false}}}`), &session); err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "s1" || !reflect.DeepEqual(session.Facets, &SessionFacets{Turn: &TurnState{}}) {
		t.Fatalf("decoded session %q facets = %+v", session.SessionID, session.Facets)
	}
}
