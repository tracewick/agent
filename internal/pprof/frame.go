package pprof

import (
	"strconv"
	"strings"
)

// Frame is one sampled call-stack frame (Tracewick\Sampler\Frame).
type Frame struct {
	Function    *string
	Class       *string
	File        *string
	Line        uint32
	ClosureLine *uint32 // nil when unknown / not a closure
}

// Main is the name for top-level script body code.
const Main = "{main}"

// Name returns the aggregation name used by pprof (Frame::name).
func (f Frame) Name() string {
	function := deref(f.Function)
	if function == "" {
		return Main
	}
	if canon := f.canonicalClosure(function); canon != nil {
		return *canon
	}
	if class := deref(f.Class); class != "" {
		return class + "::" + function
	}
	return function
}

// EstimatedNameBytes is the length Name() would produce, without allocating it.
// Used by the encoder to refuse hostile closure names before concatenating paths.
func (f Frame) EstimatedNameBytes() int {
	function := deref(f.Function)
	if function == "" {
		return len(Main)
	}
	if function == "{closure}" {
		return f.estimatedClosureFromFields()
	}
	if strings.HasPrefix(function, "{closure:") && strings.HasSuffix(function, "}") {
		return f.estimatedCanonicalClosure(function)
	}
	if class := deref(f.Class); class != "" {
		return len(class) + 2 + len(function)
	}
	return len(function)
}

func (f Frame) estimatedClosureFromFields() int {
	if f.ClosureLine == nil {
		if class := deref(f.Class); class != "" {
			return len(class) + len("::{closure}")
		}
		return len("{closure}")
	}
	lineDigits := decimalDigits(uint64(*f.ClosureLine))
	if class := deref(f.Class); class != "" {
		return len(class) + len("::{closure:") + lineDigits + 1
	}
	return len("{closure:") + len(deref(f.File)) + 1 + lineDigits + 1
}

func (f Frame) estimatedCanonicalClosure(function string) int {
	inner := function[len("{closure:") : len(function)-1]
	colon := strings.LastIndex(inner, ":")
	if colon < 0 {
		return len(function)
	}
	scope := inner[:colon]
	linePart := inner[colon+1:]
	if linePart == "" || len(linePart) != digitSpan(linePart) {
		return len(function)
	}
	if strings.HasSuffix(scope, "()") && strings.Contains(scope, "::") {
		return len(scope) - 2 + len("{closure:") + len(linePart) + 1
	}
	if strings.HasSuffix(scope, "()") && !strings.Contains(scope, "::") {
		return len(scope) - 2 + len("{closure:") + len(linePart) + 1
	}
	if class := deref(f.Class); class != "" {
		return len(class) + len("::{closure:") + len(linePart) + 1
	}
	return len("{closure:") + len(scope) + 1 + len(linePart) + 1
}

func decimalDigits(v uint64) int {
	if v == 0 {
		return 1
	}
	n := 0
	for v > 0 {
		v /= 10
		n++
	}
	return n
}

func (f Frame) canonicalClosure(function string) *string {
	if function == "{closure}" {
		s := f.closureFromFields()
		return &s
	}
	if !strings.HasPrefix(function, "{closure:") || !strings.HasSuffix(function, "}") {
		return nil
	}
	inner := function[len("{closure:") : len(function)-1]
	colon := strings.LastIndex(inner, ":")
	if colon < 0 {
		return &function
	}
	scope := inner[:colon]
	linePart := inner[colon+1:]
	if linePart == "" || len(linePart) != digitSpan(linePart) {
		return &function
	}
	line := linePart
	if strings.HasSuffix(scope, "()") && strings.Contains(scope, "::") {
		s := scope[:len(scope)-2] + "{closure:" + line + "}"
		return &s
	}
	if strings.HasSuffix(scope, "()") && !strings.Contains(scope, "::") {
		s := scope[:len(scope)-2] + "{closure:" + line + "}"
		return &s
	}
	if class := deref(f.Class); class != "" {
		s := class + "::{closure:" + line + "}"
		return &s
	}
	s := "{closure:" + scope + ":" + line + "}"
	return &s
}

func (f Frame) closureFromFields() string {
	if f.ClosureLine == nil {
		if class := deref(f.Class); class != "" {
			return class + "::{closure}"
		}
		return "{closure}"
	}
	line := strconv.FormatUint(uint64(*f.ClosureLine), 10)
	if class := deref(f.Class); class != "" {
		return class + "::{closure:" + line + "}"
	}
	return "{closure:" + deref(f.File) + ":" + line + "}"
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func digitSpan(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n++
	}
	return n
}

func u32Ptr(v uint32) *uint32 { return &v }
