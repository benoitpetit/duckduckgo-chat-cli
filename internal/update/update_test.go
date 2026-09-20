package update

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
	}{
		{left: "1.5.1", right: "1.5.0", want: 1},
		{left: "v1.5.0", right: "1.5.0", want: 0},
		{left: "1.4.9", right: "1.5.0", want: -1},
	}
	for _, tt := range tests {
		got, err := compareVersions(tt.left, tt.right)
		if err != nil || got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d, %v; want %d", tt.left, tt.right, got, err, tt.want)
		}
	}
}

func TestVersionNeedsUpdateDoesNotDowngrade(t *testing.T) {
	if versionNeedsUpdate("1.6.0", "1.5.1") {
		t.Fatal("updater should not downgrade a newer local version")
	}
	if !versionNeedsUpdate("dev", "1.5.1") {
		t.Fatal("development builds should check for updates")
	}
}
