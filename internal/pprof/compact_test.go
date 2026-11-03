package pprof_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/tracewick/agent/internal/pprof"
	"github.com/tracewick/agent/internal/spool"
)

func TestCompactStacksRoundTripFromSpool(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"web-request.spool", "edge-cases.spool"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := readSpool(t, name)
			frames, stacks, err := pprof.DecodeCompactStacks(f.Payload)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := pprof.EncodeCompactStacks(frames, stacks)
			if err != nil {
				t.Fatal(err)
			}
			frames2, stacks2, err := pprof.DecodeCompactStacks(encoded)
			if err != nil {
				t.Fatal(err)
			}
			assertFramesEqual(t, frames, frames2)
			assertStacksEqual(t, stacks, stacks2)
		})
	}
}

func TestCompactStacksNullFieldsAndTruncation(t *testing.T) {
	t.Parallel()
	fn := "fn"
	class := "Cls"
	file := "/a.php"
	cl := uint32(11)
	frames := []pprof.Frame{
		{},
		{Function: &fn, Class: &class, File: &file, Line: 10, ClosureLine: &cl},
	}
	stacks := []pprof.Stack{{FrameIDs: []int{0, 1}, Count: 5, Truncated: true}}
	encoded, err := pprof.EncodeCompactStacks(frames, stacks)
	if err != nil {
		t.Fatal(err)
	}
	gotFrames, gotStacks, err := pprof.DecodeCompactStacks(encoded)
	if err != nil {
		t.Fatal(err)
	}
	assertFramesEqual(t, frames, gotFrames)
	assertStacksEqual(t, stacks, gotStacks)
}

func TestCompactStacksRejects(t *testing.T) {
	t.Parallel()
	if _, _, err := pprof.DecodeCompactStacks([]byte{9}); err == nil {
		t.Fatal("schema")
	}
	ok, err := pprof.EncodeCompactStacks([]pprof.Frame{{Function: str("a")}}, []pprof.Stack{{FrameIDs: []int{0}, Count: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pprof.DecodeCompactStacks(append(append([]byte{}, ok...), 0)); err == nil {
		t.Fatal("trailing")
	}
	if _, _, err := pprof.DecodeCompactStacks([]byte{1, 0, 0, 0, 1}); err == nil {
		t.Fatal("truncated")
	}
	if _, err := pprof.EncodeCompactStacks(nil, []pprof.Stack{{FrameIDs: []int{0}, Count: 1}}); err == nil {
		t.Fatal("missing frame")
	}
	// Schema 1, 0 strings, 1 stack with string index 0.
	payload := []byte{1}
	payload = appendU32(payload, 0, 1, 1)
	payload = append(payload, 0)
	payload = appendU32(payload, 1, 0, 0xFFFFFFFF, 0xFFFFFFFF, 1, 0xFFFFFFFF)
	if _, _, err := pprof.DecodeCompactStacks(payload); err == nil {
		t.Fatal("string index")
	}
}

func readSpool(t *testing.T, name string) *spool.File {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", name))
	if err != nil {
		t.Fatal(err)
	}
	f, err := spool.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func appendU32(out []byte, values ...uint32) []byte {
	var buf [4]byte
	for _, v := range values {
		binary.BigEndian.PutUint32(buf[:], v)
		out = append(out, buf[:]...)
	}
	return out
}

func assertFramesEqual(t *testing.T, a, b []pprof.Frame) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("frames len %d != %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Name() != b[i].Name() || a[i].Line != b[i].Line {
			t.Fatalf("frame %d: %#v vs %#v", i, a[i], b[i])
		}
		if deref(a[i].Function) != deref(b[i].Function) ||
			deref(a[i].Class) != deref(b[i].Class) ||
			deref(a[i].File) != deref(b[i].File) {
			t.Fatalf("frame %d fields: %#v vs %#v", i, a[i], b[i])
		}
		if (a[i].ClosureLine == nil) != (b[i].ClosureLine == nil) {
			t.Fatalf("frame %d closure: %#v vs %#v", i, a[i], b[i])
		}
		if a[i].ClosureLine != nil && *a[i].ClosureLine != *b[i].ClosureLine {
			t.Fatalf("frame %d closure line", i)
		}
	}
}

func assertStacksEqual(t *testing.T, a, b []pprof.Stack) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("stacks len %d != %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Count != b[i].Count || a[i].Truncated != b[i].Truncated || !bytes.Equal(intSliceBytes(a[i].FrameIDs), intSliceBytes(b[i].FrameIDs)) {
			t.Fatalf("stack %d: %#v vs %#v", i, a[i], b[i])
		}
	}
}

func intSliceBytes(ids []int) []byte {
	var b []byte
	for _, id := range ids {
		b = appendU32(b, uint32(id)) //nolint:gosec // test frame ids are tiny
	}
	return b
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
