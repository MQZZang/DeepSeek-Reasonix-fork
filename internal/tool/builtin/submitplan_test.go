package builtin

import (
	"context"
	"strings"
	"testing"
)

// TestSubmitPlanValidatesShape pins the tool-side contract of the structured
// plan submission (PR4): well-formed args ack with title/phase/step counts,
// malformed args error so the model can correct itself, and the tool is
// read-only so it stays callable under the plan-mode ceiling.
func TestSubmitPlanValidatesShape(t *testing.T) {
	sp := submitPlan{}
	if !sp.ReadOnly() {
		t.Fatal("submit_plan must be ReadOnly so the plan-mode ceiling admits it")
	}

	out, err := sp.Execute(context.Background(), []byte(`{
		"title":"Fix the add bug",
		"phases":[
			{"name":"Reproduce","steps":["write failing test"]},
			{"name":"Fix","steps":["correct the operator","run tests"]}
		]}`))
	if err != nil {
		t.Fatalf("valid submission: %v", err)
	}
	if !strings.Contains(out, `"Fix the add bug"`) || !strings.Contains(out, "2 phases") || !strings.Contains(out, "3 steps") {
		t.Fatalf("ack = %q, want title, phase count and step count echoed", out)
	}

	for name, args := range map[string]string{
		"missing title":      `{"phases":[{"name":"A"}]}`,
		"blank title":        `{"title":"  ","phases":[{"name":"A"}]}`,
		"no phases":          `{"title":"T","phases":[]}`,
		"blank phase name":   `{"title":"T","phases":[{"name":" "}]}`,
		"not json":           `{"title":`,
	} {
		if _, err := sp.Execute(context.Background(), []byte(args)); err == nil {
			t.Errorf("%s: Execute succeeded, want validation error", name)
		}
	}
}
