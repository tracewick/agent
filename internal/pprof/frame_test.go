package pprof_test

import (
	"testing"

	"github.com/tracewick/agent/internal/pprof"
)

func TestFrameName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		f    pprof.Frame
		want string
	}{
		{"function", fr(str("strlen"), nil, nil, 0, nil), "strlen"},
		{"method", fr(str("handle"), str(`App\Kernel`), str("/app/src/Kernel.php"), 42, nil), `App\Kernel::handle`},
		{"top-level", fr(nil, nil, str("/app/public/index.php"), 5, nil), pprof.Main},
		{"empty function", fr(str(""), str(`App\Kernel`), nil, 0, nil), pprof.Main},
		{"empty class", fr(str("helper"), str(""), nil, 0, nil), "helper"},
		{"pre-8.4 top-level closure", fr(str("{closure}"), nil, str("/app/src/routes.php"), 12, u32(10)), "{closure:/app/src/routes.php:10}"},
		{"pre-8.4 bound closure", fr(str("{closure}"), str(`App\Kernel`), str("/app/src/Kernel.php"), 30, u32(28)), `App\Kernel::{closure:28}`},
		{"pre-8.4 no line", fr(str("{closure}"), nil, str("/app/src/routes.php"), 12, nil), "{closure}"},
		{"pre-8.4 bound no line", fr(str("{closure}"), str(`App\Kernel`), str("/app/src/Kernel.php"), 30, nil), `App\Kernel::{closure}`},
		{"pre-8.4 no file", fr(str("{closure}"), nil, nil, 0, u32(7)), "{closure::7}"},
		{"relative path", fr(str("{closure}"), nil, str("src/routes.php"), 12, u32(10)), "{closure:src/routes.php:10}"},
		{"8.4 method closure", fr(str(`{closure:App\Foo::bar():12}`), str(`App\Foo`), str("/app/src/Foo.php"), 15, u32(12)), `App\Foo::bar{closure:12}`},
		{"8.4 method ignores class", fr(str(`{closure:App\Foo::bar():12}`), nil, str("/app/src/Foo.php"), 15, u32(12)), `App\Foo::bar{closure:12}`},
		{"8.4 function closure", fr(str("{closure:helper():9}"), nil, str("/app/helpers.php"), 11, u32(9)), "helper{closure:9}"},
		{"8.4 path closure", fr(str("{closure:/app/src/routes.php:10}"), nil, str("/app/src/routes.php"), 12, u32(10)), "{closure:/app/src/routes.php:10}"},
		{"8.4 path with class", fr(str("{closure:/app/src/Kernel.php:28}"), str(`App\Kernel`), str("/app/src/Kernel.php"), 30, u32(28)), `App\Kernel::{closure:28}`},
		{"8.4 relative", fr(str("{closure:src/routes.php:10}"), nil, str("src/routes.php"), 12, u32(10)), "{closure:src/routes.php:10}"},
		{"malformed no colon", fr(str("{closure:nocolon}"), nil, str("/app/x.php"), 1, nil), "{closure:nocolon}"},
		{"malformed non-numeric", fr(str("{closure:scope:abc}"), nil, str("/app/x.php"), 1, nil), "{closure:scope:abc}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.f.Name(); got != tc.want {
				t.Fatalf("Name() = %q, want %q", got, tc.want)
			}
		})
	}
}

func fr(function, class, file *string, line uint32, closure *uint32) pprof.Frame {
	return pprof.Frame{Function: function, Class: class, File: file, Line: line, ClosureLine: closure}
}

func str(s string) *string { return &s }

func u32(v uint32) *uint32 { return &v }
