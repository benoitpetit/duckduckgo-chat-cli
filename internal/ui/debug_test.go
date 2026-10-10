package ui

import "testing"

func TestDebugEnabledUsesExistingEnvironmentSwitch(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "true", want: true},
		{value: "false", want: false},
		{value: "1", want: false},
	} {
		t.Setenv("DEBUG", test.value)
		if got := DebugEnabled(); got != test.want {
			t.Errorf("DebugEnabled() with DEBUG=%q = %t, want %t", test.value, got, test.want)
		}
	}
}
