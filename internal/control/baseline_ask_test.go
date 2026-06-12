package control

// Gate-2 baseline cases A1–A3: the "Ask" (read-only Q&A) scenario. Since PR2
// Reasonix has a dedicated ask mode (SetCollaborationMode(CollabAsk) / "/ask"):
// A1/A2 pin what the normal (agent) surface does when the user's intent is a
// question, and the *AskMode* tests pin the resolved contract — a harness
// read-only ceiling with zero approval friction. See baseline_helpers_test.go
// for the suite conventions and the KNOWN-GAP marker contract.

import (
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestBaselineA1ReadOnlyQuestionNoApproval — Gate-2 case A1.
//
// A pure question answered with read-only tools must run friction-free: the
// permission fallback for ReadOnly()==true tools is Allow regardless of policy
// mode (internal/permission/permission.go DecideSubject), so no ApprovalRequest
// may be emitted and the tool must execute.
func TestBaselineA1ReadOnlyQuestionNoApproval(t *testing.T) {
	reader := &fakeTool{name: "read_file", readOnly: true}
	reg := tool.NewRegistry()
	reg.Add(reader)

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "read_file", `{"path":"internal/boot/boot.go"}`),
		textTurn("boot.Build is the single assembly point shared by every frontend."),
	}}
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)

	b, sink := newBaselineEvents()
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Policy:   permission.New("ask", nil, nil, nil),
		Sink:     sink,
	})
	c.EnableInteractiveApproval()
	requested := autoRespondApprovals(t, c, b, func(event.Approval) bool { return true })

	runTurnOK(t, c, "explain what boot.Build does")

	if got := len(requested()); got != 0 {
		t.Fatalf("approval prompts = %d, want 0 (read-only fallback is allow)", got)
	}
	if got := reader.callCount(); got != 1 {
		t.Fatalf("read_file executions = %d, want 1", got)
	}
}

// TestBaselineA2WriterUnderThreePostures — Gate-2 case A2, agent-mode side.
//
// The same writer tool call under the three tool-approval postures in normal
// (agent) mode. The user's *intent* here is a question ("explain X — and fix
// the typo if you spot one"), but the harness only sees a writer call:
//
//   - ask:  one ApprovalRequest; the user is the last line of defence. PASS.
//   - auto: zero prompts, silent write.
//   - yolo: zero prompts, silent write.
//
// RESOLVED(risk-2, PR2): Q&A now has a harness-enforced read-only home — ask
// mode (TestBaselineA2AskModeWriterBlockedUnderAllPostures below). In agent
// mode the auto/yolo silent write remains the documented posture contract —
// both are deliberate, session-scoped user opt-ins. RESOLVED(risk-5, PR6) for
// the *implicit* waiver: the approved-plan execution window no longer skips
// every prompt, only previewable file writers (plan_exec_window_test.go).
func TestBaselineA2WriterUnderThreePostures(t *testing.T) {
	cases := []struct {
		posture     string
		wantPrompts int
	}{
		{ToolApprovalAsk, 1},
		{ToolApprovalAuto, 0}, // explicit posture opt-in; pinned contract
		{ToolApprovalYolo, 0}, // explicit posture opt-in; pinned contract
	}
	for _, tc := range cases {
		t.Run(tc.posture, func(t *testing.T) {
			writer := &fakeTool{name: "edit_file", readOnly: false}
			reg := tool.NewRegistry()
			reg.Add(writer)

			prov := &scriptedTurns{turns: [][]provider.Chunk{
				toolCallTurn("c1", "edit_file", `{"path":"internal/control/input.go"}`),
				textTurn("Explained Compose — and fixed the comment typo."),
			}}
			ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)

			b, sink := newBaselineEvents()
			c := New(Options{
				Runner:   ag,
				Executor: ag,
				Policy:   permission.New("ask", nil, nil, nil),
				Sink:     sink,
			})
			c.EnableInteractiveApproval()
			c.SetToolApprovalMode(tc.posture)
			requested := autoRespondApprovals(t, c, b, func(event.Approval) bool { return true })

			runTurnOK(t, c, "explain Compose in input.go; fix the comment typo if you see one")

			if got := approvalsForTool(requested(), "edit_file"); got != tc.wantPrompts {
				t.Fatalf("posture %s: edit_file approval prompts = %d, want %d",
					tc.posture, got, tc.wantPrompts)
			}
			if got := writer.callCount(); got != 1 {
				t.Fatalf("posture %s: edit_file executions = %d, want 1 (write happens under every posture today)",
					tc.posture, got)
			}
		})
	}
}

// TestBaselineA3PlanModeQADoesNotGate — Gate-2 case A3, plan-mode side
// (resolved in PR4).
//
// The plan approval gate keys on an explicit submit_plan tool call, not on
// the reply text: a plain answer in plan mode is conversation and ends the
// turn with zero ApprovalRequests, keeping plan mode on for the next turn.
// (The old behaviour — every non-empty reply gated — was the A3 KNOWN-GAP.)
func TestBaselineA3PlanModeQADoesNotGate(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Agent.Run stops when the model returns no tool calls and the readiness gate passes."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)

	b, sink := newBaselineEvents()
	c := New(Options{Runner: ag, Executor: ag, Sink: sink})
	requested := autoRespondApprovals(t, c, b, func(event.Approval) bool { return false })

	c.SetPlanMode(true)
	runTurnOK(t, c, "what are the stop conditions of the agent loop?")

	if got := len(requested()); got != 0 {
		t.Fatalf("plan-mode Q&A raised %d approvals, want 0 (gate fires only on submit_plan)", got)
	}
	if !c.PlanMode() {
		t.Fatal("answering a question must keep plan mode on")
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1 (no nudge for a non-plan-shaped answer)", prov.call)
	}
}

// TestBaselineA3PlanSubmissionRaisesGate — Gate-2 case A3 companion (PR4).
//
// The flip side of the contract above: an explicit submit_plan call in the
// same plan-mode session raises exactly one exit_plan_mode ApprovalRequest,
// and a denial keeps plan mode on.
func TestBaselineA3PlanSubmissionRaisesGate(t *testing.T) {
	submit := &fakeTool{name: "submit_plan", readOnly: true}
	reg := tool.NewRegistry()
	reg.Add(submit)
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "submit_plan", `{"title":"Fix the loop guard","phases":[{"name":"Diagnose","steps":["pin the failing case"]},{"name":"Fix","steps":["adjust the threshold"]}]}`),
		textTurn("Plan:\n1. Diagnose\n   - pin the failing case\n2. Fix\n   - adjust the threshold"),
	}}
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, event.Discard)

	b, sink := newBaselineEvents()
	c := New(Options{Runner: ag, Executor: ag, Sink: sink})
	requested := autoRespondApprovals(t, c, b, func(event.Approval) bool { return false })

	c.SetPlanMode(true)
	runTurnOK(t, c, "plan the loop-guard fix")

	if got := approvalsForTool(requested(), planApprovalTool); got != 1 {
		t.Fatalf("submit_plan raised %d %q approvals, want exactly 1", got, planApprovalTool)
	}
	if !c.PlanMode() {
		t.Fatal("denying the gate must keep plan mode on")
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2 (tool turn + reply turn, no execution after denial)", prov.call)
	}
}

// TestBaselineA2AskModeWriterBlockedUnderAllPostures — Gate-2 case A2, resolved
// side (PR2, permanent contract).
//
// In ask mode the read-only capability ceiling binds BEFORE the permission
// gate under every approval posture (ceiling ∧ approval): the writer never
// runs, no ApprovalRequest is raised — not even under the "ask" posture — the
// model receives the canonical blocked notice, the turn completes, and the
// session stays in ask mode.
func TestBaselineA2AskModeWriterBlockedUnderAllPostures(t *testing.T) {
	for _, posture := range []string{ToolApprovalAsk, ToolApprovalAuto, ToolApprovalYolo} {
		t.Run(posture, func(t *testing.T) {
			writer := &fakeTool{name: "edit_file", readOnly: false}
			reg := tool.NewRegistry()
			reg.Add(writer)

			prov := &scriptedTurns{turns: [][]provider.Chunk{
				toolCallTurn("c1", "edit_file", `{"path":"internal/control/input.go"}`),
				textTurn("Compose prepends the mode marker. Fixing the typo needs agent mode — switch when ready."),
			}}
			// Shared sink, as boot wires it: ToolResult comes from the agent.
			b, sink := newBaselineEvents()
			ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, sink)
			c := New(Options{
				Runner:   ag,
				Executor: ag,
				Policy:   permission.New("ask", nil, nil, nil),
				Sink:     sink,
			})
			c.EnableInteractiveApproval()
			c.SetToolApprovalMode(posture)
			requested := autoRespondApprovals(t, c, b, func(event.Approval) bool { return true })
			c.SetCollaborationMode(CollabAsk)

			runTurnOK(t, c, "explain Compose in input.go; fix the comment typo if you see one")

			if got := approvalsForTool(requested(), "edit_file"); got != 0 {
				t.Fatalf("posture %s: edit_file approval prompts = %d, want 0 (ceiling blocks before the gate)", posture, got)
			}
			if got := writer.callCount(); got != 0 {
				t.Fatalf("posture %s: edit_file executions = %d, want 0 in ask mode", posture, got)
			}
			results := resultsForTool(drainToolResults(b), "edit_file")
			if len(results) != 1 || results[0].Err != "blocked: read-only mode" {
				t.Fatalf("posture %s: edit_file results = %+v, want exactly one blocked result", posture, results)
			}
			if got := c.CollaborationMode(); got != CollabAsk {
				t.Fatalf("collaboration mode after the turn = %q, want %q (answering must not exit ask mode)", got, CollabAsk)
			}
		})
	}
}

// TestBaselineA3AskModeQANoApprovalGate — Gate-2 case A3, resolved side (PR2,
// permanent contract).
//
// A text answer in ask mode ends the turn with zero ApprovalRequests of any
// kind — no exit_plan_mode gate, no tool prompts — and the session stays in
// ask mode for the next question.
func TestBaselineA3AskModeQANoApprovalGate(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Agent.Run stops when the model returns no tool calls and the readiness gate passes."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)

	b, sink := newBaselineEvents()
	c := New(Options{Runner: ag, Executor: ag, Sink: sink})
	requested := autoRespondApprovals(t, c, b, func(event.Approval) bool { return false })

	c.SetCollaborationMode(CollabAsk)
	runTurnOK(t, c, "what are the stop conditions of the agent loop?")

	if got := len(requested()); got != 0 {
		t.Fatalf("ask-mode Q&A raised %d approvals, want 0", got)
	}
	if got := c.CollaborationMode(); got != CollabAsk {
		t.Fatalf("collaboration mode after the turn = %q, want %q", got, CollabAsk)
	}
	if c.PlanMode() {
		t.Fatal("ask mode must not report plan mode")
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1 (answer ends the turn)", prov.call)
	}
}
