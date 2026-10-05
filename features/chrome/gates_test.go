package chrome

import (
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// scopedSettings is exttest settings plus a managed (policy) scope.
type scopedSettings struct {
	*exttest.Settings
	policy map[string]any
}

func (s scopedSettings) ClaudeScope(scope string) map[string]any {
	if scope == ext.ScopePolicy {
		return s.policy
	}
	return nil
}
func (s scopedSettings) ClaudeSources() []ext.SettingsSource { return nil }

func TestStatusLineManagedGates(t *testing.T) {
	user := map[string]any{"command": "user.sh"}
	managed := map[string]any{"command": "managed.sh"}
	tests := []struct {
		name   string
		merged map[string]any
		policy map[string]any
		want   string
	}{
		{"plain", map[string]any{"statusLine": user}, nil, "user.sh"},
		{"disableAllHooks, no managed", map[string]any{"statusLine": user, "disableAllHooks": true}, nil, ""},
		{"disableAllHooks, managed", map[string]any{"statusLine": user, "disableAllHooks": true},
			map[string]any{"statusLine": managed}, "managed.sh"},
		{"allowManagedHooksOnly", map[string]any{"statusLine": user},
			map[string]any{"allowManagedHooksOnly": true}, ""},
		{"managed disables all", map[string]any{"statusLine": managed},
			map[string]any{"disableAllHooks": true, "statusLine": managed}, ""},
	}
	for _, tt := range tests {
		s := scopedSettings{exttest.NewSettings(tt.merged), tt.policy}
		if got := statusLineConfig(s).Command; got != tt.want {
			t.Errorf("%s: command = %q, want %q", tt.name, got, tt.want)
		}
	}
}
