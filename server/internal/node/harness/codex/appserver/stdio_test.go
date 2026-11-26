package appserver

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type codexStdioTestInput struct{ bytes.Buffer }

func (*codexStdioTestInput) Close() error { return nil }

func TestCodexStdioSupportsModelListAndPreservesRPCErrors(t *testing.T) {
	t.Parallel()
	in := &codexStdioTestInput{}
	s := &Stdio{ctx: context.Background(), cancel: func() {}, in: in,
		lines: bufio.NewScanner(strings.NewReader(`{"method":"thread/status","params":{}}
{"id":99,"result":{}}
{"id":1,"result":{"data":[{"model":"gpt-example"}],"nextCursor":null}}
{"id":2,"error":{"code":-32601,"message":"unknown method"}}
`))}
	var models struct {
		Data []struct {
			Model string `json:"model"`
		} `json:"data"`
	}
	if err := s.Call(context.Background(), "model/list", map[string]any{"includeHidden": true}, &models); err != nil ||
		len(models.Data) != 1 || models.Data[0].Model != "gpt-example" {
		t.Fatalf("model/list = %+v, %v", models, err)
	}
	err := s.Call(context.Background(), "project/list", map[string]any{}, nil)
	var rpcErr *StdioError
	if !errors.As(err, &rpcErr) || rpcErr.Method != "project/list" || rpcErr.Code != -32601 {
		t.Fatalf("lost native RPC error: %v", err)
	}
	if !strings.Contains(in.String(), `"method":"model/list","params":{"includeHidden":true}`) {
		t.Fatalf("model request changed: %s", in.String())
	}
}
