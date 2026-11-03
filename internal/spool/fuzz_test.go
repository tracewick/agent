package spool_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tracewick/agent/internal/spool"
)

func FuzzParse(f *testing.F) {
	dir := filepath.Join("..", "..", "testdata", "spool", "v2")
	entries, err := os.ReadDir(dir)
	if err != nil {
		f.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".spool" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if readErr != nil {
			f.Fatal(readErr)
		}
		f.Add(data)
	}
	f.Add([]byte(nil))
	f.Add([]byte("TWSP"))
	f.Add([]byte("TWSP\x02\x00\x00\x00\x02{}"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = spool.Parse(data) // must not panic
	})
}
