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
