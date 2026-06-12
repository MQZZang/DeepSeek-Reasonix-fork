package builtin

import (
	"encoding/json"
	"testing"
)

// TestBashIsReadOnlyCommand pins the bash tool's commandReadOnly contract
// (PR3): IsReadOnlyCommand must agree with the permission package classifier
// for foreground commands, and must classify background runs and malformed
// args conservatively (false), because the ceiling check admits a call past
// CeilingReadOnly purely on this answer.
func TestBashIsReadOnlyCommand(t *testing.T) {
	tests := []struct {
		name string
		args string
		want bool
	}{
		// Read-only diagnostics admitted under a read-only ceiling.
		{"git log", `{"command":"git log -5"}`, true},
		{"git diff", `{"command":"git diff HEAD~1"}`, true},
		{"git status", `{"command":"git status"}`, true},
		{"go vet", `{"command":"go vet ./..."}`, true},
		{"grep", `{"command":"grep -rn TODO ."}`, true},
		{"ls", `{"command":"ls -la"}`, true},

		// Writers and side-effecting commands stay blocked.
		{"rm -rf", `{"command":"rm -rf out"}`, false},
		{"git commit", `{"command":"git commit -m msg"}`, false},
		{"git push", `{"command":"git push"}`, false},
		{"go build", `{"command":"go build ./..."}`, false},
		{"npm install", `{"command":"npm install"}`, false},

		// Shell syntax can smuggle side effects — conservative.
		{"pipe to tee", `{"command":"cat main.go | tee copy.go"}`, false},
		{"redirect", `{"command":"git diff > changes.patch"}`, false},
		{"chained rm", `{"command":"ls; rm file.txt"}`, false},

		// Background jobs escape the turn — conservative even when read-only.
		{"background git log", `{"command":"git log -5","run_in_background":true}`, false},

		// Malformed args — conservative.
		{"invalid json", `{"command":`, false},
		{"empty", `{}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (bash{}).IsReadOnlyCommand(json.RawMessage(tt.args)); got != tt.want {
				t.Errorf("bash.IsReadOnlyCommand(%s) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
