package node

import (
	"os"
	"testing"
)

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString(s)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("append: write=%v close=%v", writeErr, closeErr)
	}
}
