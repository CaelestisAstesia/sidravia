package invocation

import "testing"

func TestReadOnlyMaintenanceQueries(t *testing.T) {
	for _, item := range []struct {
		args    []string
		allowed bool
	}{
		{nil, true}, {[]string{"status"}, true}, {[]string{"version"}, true},
		{[]string{"help"}, true}, {[]string{"--help"}, true},
		{[]string{"recover", "--operation", "id"}, false},
		{[]string{"check"}, false}, {[]string{"download"}, false},
		{[]string{"apply", "--confirm"}, false}, {[]string{"unknown"}, false},
	} {
		if got := ReadOnly(item.args); got != item.allowed {
			t.Fatalf("%v: got %v", item.args, got)
		}
	}
}
