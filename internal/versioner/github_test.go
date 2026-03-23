package versioner

import (
	"testing"
)

func TestParseVersionFromTag(t *testing.T) {
	tests := []struct {
		tag    string
		prefix string
		want   string
	}{
		{"v1.32.0", "v", "v1.32.0"},
		{"release-1.0.0", "release-", "release-1.0.0"},
		{"1.0.0", "", "1.0.0"},
	}

	for _, tt := range tests {
		got := parseVersionFromTag(tt.tag, tt.prefix)
		if got != tt.want {
			t.Errorf("parseVersionFromTag(%q, %q) = %q, want %q", tt.tag, tt.prefix, got, tt.want)
		}
	}
}
