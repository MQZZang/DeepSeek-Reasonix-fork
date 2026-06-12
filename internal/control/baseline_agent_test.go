package control

// Gate-2 baseline case D2: deny/failure backoff in the default Agent mode.
// D1/D4 (golden path, rewind) are pinned by e2ebench fix-add-bug and
// rewind_e2e_test.go. This file pins the two loop-guard directions discovered
// in Gate 1: errors arm the storm breaker, denials deliberately do not.
// See baseline_helpers_test.go for suite conventions.

import (
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestBaselineD2ErrorStormTriggersLoopGuard — Gate-2 case D2, direction (a).
//
// Three consecutive batches failing with the same (tool, error) signature arm
// the storm breaker (agent.go applyStormBreaker, stormBreakThreshold=3): the
// third result fed back to the MODEL is rewritten with the [loop guard]
// instruction and a warn-level Notice is emitted. Note the seam: the rewrite
// happens after ToolResult events are emitted (executeBatch), so the UI card
// shows the raw error while the session transcript carries the rewrite — the
// session message is the contract surface asserted here. Permanent contract —
// keeps the loop from burning the whole maxSteps budget on one fixation.
func TestBaselineD2ErrorStormTriggersLoopGuard(t *testing.T) {
	failing := &fakeTool{name: "edit_file", readOnly: false, execErr: errFakeBoom}
	reg := tool.NewRegistry()
	reg.Add(failing)

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "edit_file", `{"path":"a.go"}`),
		toolCallTurn("c2", "edit_file", `{"path":"a.go"}`),
		toolCallTurn("c3", "edit_file", `{"path":"a.go"}`),
		textTurn("I am blocked: the edit keeps failing the same way."),
	}}
	b, sink := newBaselineEvents()
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, sink)
	c := New(Options{Runner: ag, Executor: ag, Sink: sink})
	// No gate: every call executes and fails; the storm breaker sees errors.

	runTurnOK(t, c, "apply the refactor")

	results := resultsForTool(drainToolResults(b), "edit_file")
	if len(results) != 3 {
		t.Fatalf("edit_file results = %d, want 3 failing attempts", len(results))
	}
	var toolMsgs []string
	for _, m := range ag.Session().Snapshot() {
		if m.Role == provider.RoleTool && m.Name == "edit_file" {
			toolMsgs = append(toolMsgs, m.Content)
		}
	}
	if len(toolMsgs) != 3 {
		t.Fatalf("session tool messages = %d, want 3", len(toolMsgs))
	}
	last := toolMsgs[2]
	if !strings.Contains(last, "[loop guard]") || !strings.Contains(last, "failed 3 times in a row") {
		t.Fatalf("third tool message to the model = %q, want the [loop guard] rewrite at stormBreakThreshold=3", last)
	}
	if !containsSubstring(drainNotices(b), "loop guard") {
		t.Fatal("storm breaker must emit a loop-guard Notice")
	}
}

// TestBaselineD2DeniedApprovalsDoNotTriggerLoopGuard — Gate-2 case D2,
// direction (b).
//
// Repeated user denials of the same writer call do NOT arm the storm breaker:
// batchStormSignature returns ok only when every call errored and none was
// merely blocked (agent.go). This is deliberate — each denial already passes
// through the user, so the user is the rate limiter and maxSteps remains the
// only backstop. Pinned as designed behaviour, not a gap.
func TestBaselineD2DeniedApprovalsDoNotTriggerLoopGuard(t *testing.T) {
	writer := &fakeTool{name: "edit_file", readOnly: false}
	reg := tool.NewRegistry()
	reg.Add(writer)

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "edit_file", `{"path":"a.go"}`),
		toolCallTurn("c2", "edit_file", `{"path":"a.go"}`),
		toolCallTurn("c3", "edit_file", `{"path":"a.go"}`),
		textTurn("Understood — stopping since the edit is not approved."),
	}}
	b, sink := newBaselineEvents()
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, sink)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Policy:   permission.New("ask", nil, nil, nil),
		Sink:     sink,
	})
	c.EnableInteractiveApproval()
	requested := autoRespondApprovals(t, c, b, func(event.Approval) bool { return false })

	runTurnOK(t, c, "apply the refactor")

	if got := writer.callCount(); got != 0 {
		t.Fatalf("denied writer executed %d times, want 0", got)
	}
	if got := approvalsForTool(requested(), "edit_file"); got != 3 {
		t.Fatalf("approval prompts = %d, want 3 (one per retry; the user is the rate limiter)", got)
	}
	results := resultsForTool(drainToolResults(b), "edit_file")
	if len(results) != 3 {
		t.Fatalf("edit_file results = %d, want 3 blocked results", len(results))
	}
	for i, r := range results {
		if !strings.HasPrefix(r.Output, "blocked: ") {
			t.Fatalf("result %d = %q, want a blocked result", i, r.Output)
		}
		if strings.Contains(r.Output, "[loop guard]") {
			t.Fatalf("result %d carries [loop guard]; blocked outcomes must not arm the storm breaker", i)
		}
	}
	if containsSubstring(drainNotices(b), "loop guard") {
		t.Fatal("denials emitted a loop-guard Notice; blocked outcomes must not arm the storm breaker")
	}
}
