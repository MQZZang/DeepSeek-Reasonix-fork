package control

// PR6 tests: the approved-plan execution window waives approval prompts only
// for previewable file writers (tool.Previewer) — the set whose changes the
// plan-approved checkpoint snapshots, so every waived write is rewindable.
// bash and other unpreviewable tools keep their normal approval flow inside
// the window, the user's denial still binds there, and the window closes when
// the execution turn ends on any path (success or failure) — no ghost waiver
// leaks into later turns. Companions: golden path (auto_plan_e2e_test.go),
// A2 postures (baseline_ask_test.go), B3 ceiling (baseline_plan_test.go).

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/tool"

	_ "reasonix/internal/tool/builtin"
)

// fakePreviewableTool augments fakeTool with tool.Previewer — the capability
// the execution window keys on: a writer whose change can be previewed and
// snapshotted into the plan-approved checkpoint before it runs.
type fakePreviewableTool struct{ *fakeTool }

func (f fakePreviewableTool) Preview(json.RawMessage) (diff.Change, error) {
	return diff.Change{Path: "fake", NewText: "x"}, nil
}

func execWindowRegistry(tools ...tool.Tool) *tool.Registry {
	reg := tool.NewRegistry()
	reg.Add(&fakeTool{name: "submit_plan", readOnly: true})
	for _, bt := range tool.Builtins() {
		if bt.Name() == "complete_step" || bt.Name() == "todo_write" {
			reg.Add(bt)
		}
	}
	for _, tl := range tools {
		reg.Add(tl)
	}
	return reg
}

// execWindowController wires a real agent behind an interactive approval gate
// around scripted turns, in plan mode, with a fake submit_plan plus the given
// tools registered. The registry is shared with the controller so the window
// can consult tool capabilities.
func execWindowController(t *testing.T, prov *scriptedTurns, decide func(event.Approval) bool, tools ...tool.Tool) (*Controller, func() []event.Approval) {
	t.Helper()
	reg := execWindowRegistry(tools...)
	b, sink := newBaselineEvents()
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, sink)
	c := New(Options{
		Runner:        ag,
		Executor:      ag,
		Registry:      reg,
		Policy:        permission.New("ask", nil, nil, nil),
		Sink:          sink,
		WorkspaceRoot: t.TempDir(),
	})
	c.EnableInteractiveApproval()
	requested := autoRespondApprovals(t, c, b, decide)
	c.SetPlanMode(true)
	return c, requested
}

// runTurnErr drives one turn and returns its error (runTurnOK's sibling for
// paths that are expected to fail).
func runTurnErr(t *testing.T, c *Controller, input string) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.runTurnWithRaw(context.Background(), input, input) }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("runTurnWithRaw(%q) timed out", input)
		return nil
	}
}

const planExecTodosDone = `{"todos":[{"content":"Reproduce","status":"completed","level":0},{"content":"write failing test","status":"completed","level":1},{"content":"Fix","status":"completed","level":0},{"content":"correct the operator","status":"completed","level":1}]}`

// just-approved plan, a previewable file writer runs without an approval
// prompt — approving the plan was the approval for those edits. The waiver
// lasts exactly that turn: the same writer prompts again on the next turn.
func TestPlanExecWindowWaivesPreviewableWriters(t *testing.T) {
	writer := &fakeTool{name: "write_file", readOnly: false}
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "submit_plan", planSubmitArgs),
		textTurn("Plan:\n1. Reproduce\n2. Fix"),
		toolCallTurn("e1", "write_file", `{"path":"internal/calc/add.go"}`),
		toolCallTurn("e1-todo", "todo_write", planExecTodosDone),
		textTurn("Done — fixed."),
		// Next turn, outside the window: the same writer must prompt again.
		toolCallTurn("e2", "write_file", `{"path":"internal/calc/add_test.go"}`),
		textTurn("Added the regression test."),
	}}
	c, requested := execWindowController(t, prov, func(event.Approval) bool { return true },
		fakePreviewableTool{writer})

	runTurnOK(t, c, "plan and fix the add bug")

	if got := approvalsForTool(requested(), planApprovalTool); got != 1 {
		t.Fatalf("plan approvals = %d, want 1", got)
	}
	if got := approvalsForTool(requested(), "write_file"); got != 0 {
		t.Fatalf("write_file prompts inside the window = %d, want 0 (previewable writers are waived)", got)
	}
	if got := writer.callCount(); got != 1 {
		t.Fatalf("write_file executions = %d, want 1", got)
	}
	if c.PlanMode() {
		t.Fatal("plan mode should be off after approval")
	}

	// The window closed with the execution turn: the next write prompts.
	runTurnOK(t, c, "now add a regression test")
	if got := approvalsForTool(requested(), "write_file"); got != 1 {
		t.Fatalf("write_file prompts after the window = %d, want 1 (no ghost waiver)", got)
	}
	if got := writer.callCount(); got != 2 {
		t.Fatalf("write_file executions = %d, want 2", got)
	}
}

// TestPlanExecWindowStillGatesBash — the B1 companion assertion (risk-5).
//
// bash does not implement tool.Previewer (its targets are unknowable and the
// checkpoint cannot snapshot them), so inside the execution window a bash
// call with side effects still raises its normal approval prompt; allowing it
// lets it run. Read-only bash commands are unaffected by the window either
// way — permission.Gate.Check reclassifies them before the approver is asked.
func TestPlanExecWindowStillGatesBash(t *testing.T) {
	bash := &fakeTool{name: "bash", readOnly: false}
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "submit_plan", planSubmitArgs),
		textTurn("Plan:\n1. Reproduce\n2. Fix"),
		toolCallTurn("e1", "bash", `{"command":"go test ./internal/calc/"}`),
		toolCallTurn("e1-todo", "todo_write", planExecTodosDone),
		textTurn("Done — tests pass."),
	}}
	c, requested := execWindowController(t, prov, func(event.Approval) bool { return true }, bash)

	runTurnOK(t, c, "plan and fix the add bug")

	if got := approvalsForTool(requested(), "bash"); got != 1 {
		t.Fatalf("bash prompts inside the window = %d, want 1 (unpreviewable side effects still gate)", got)
	}
	if got := bash.callCount(); got != 1 {
		t.Fatalf("bash executions = %d, want 1 (allowed after the prompt)", got)
	}
}

// TestPlanExecWindowDeniedBashDoesNotRun: the user's denial still binds inside
// the window — approving the plan never pre-approves a side-effect command.
func TestPlanExecWindowDeniedBashDoesNotRun(t *testing.T) {
	bash := &fakeTool{name: "bash", readOnly: false}
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "submit_plan", planSubmitArgs),
		textTurn("Plan:\n1. Reproduce\n2. Fix"),
		toolCallTurn("e1", "bash", `{"command":"rm -rf build/"}`),
		toolCallTurn("e1-todo", "todo_write", planExecTodosDone),
		textTurn("Understood — stopping here."),
	}}
	c, requested := execWindowController(t, prov, func(a event.Approval) bool {
		return a.Tool == planApprovalTool // approve the plan, deny the bash call
	}, bash)

	runTurnOK(t, c, "plan and clean up the build dir")

	if got := approvalsForTool(requested(), "bash"); got != 1 {
		t.Fatalf("bash prompts = %d, want 1", got)
	}
	if got := bash.callCount(); got != 0 {
		t.Fatalf("bash executions after denial = %d, want 0 (the user is still the last line of defence)", got)
	}
}

// failingExecutionRunner forwards Run to the real agent except for the
// failAt-th call (1-based), which fails immediately — simulating an execution
// turn that dies (provider failure, cancellation) so the window-reset defer's
// error path is exercised.
type failingExecutionRunner struct {
	ag     *agent.Agent
	calls  int
	failAt int
}

func (r *failingExecutionRunner) Run(ctx context.Context, input string) error {
	r.calls++
	if r.calls == r.failAt {
		return errFakeBoom
	}
	return r.ag.Run(ctx, input)
}

// TestPlanExecWindowClosesWhenExecutionTurnFails: a failing execution turn
// resets the window on the way out — no ghost waiver survives into the next
// turn, where the writer prompts as normal.
func TestPlanExecWindowClosesWhenExecutionTurnFails(t *testing.T) {
	writer := &fakeTool{name: "write_file", readOnly: false}
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "submit_plan", planSubmitArgs),
		textTurn("Plan:\n1. Reproduce\n2. Fix"),
		// The failing runner preempts the execution turn; these turns serve
		// the post-failure turn instead.
		toolCallTurn("e2", "write_file", `{"path":"internal/calc/add.go"}`),
		toolCallTurn("e2-todo", "todo_write", planExecTodosDone),
		textTurn("Wrote the fix."),
	}}

	reg := execWindowRegistry(fakePreviewableTool{writer})
	b, sink := newBaselineEvents()
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, sink)
	runner := &failingExecutionRunner{ag: ag, failAt: 2} // call 2 = the post-approval execution turn
	c := New(Options{
		Runner:        runner,
		Executor:      ag,
		Registry:      reg,
		Policy:        permission.New("ask", nil, nil, nil),
		Sink:          sink,
		WorkspaceRoot: t.TempDir(),
	})
	c.EnableInteractiveApproval()
	requested := autoRespondApprovals(t, c, b, func(event.Approval) bool { return true })
	c.SetPlanMode(true)

	if err := runTurnErr(t, c, "plan and fix the add bug"); !errors.Is(err, errFakeBoom) {
		t.Fatalf("turn error = %v, want errFakeBoom from the execution turn", err)
	}
	c.mu.Lock()
	window := c.approvedPlanAutoApproveTools
	c.mu.Unlock()
	if window {
		t.Fatal("execution window must reset when the execution turn fails")
	}

	// And the next turn sees no leftover waiver: the writer prompts.
	runTurnOK(t, c, "write the fix anyway")
	if got := approvalsForTool(requested(), "write_file"); got != 1 {
		t.Fatalf("write_file prompts after the failed window = %d, want 1", got)
	}
	if got := writer.callCount(); got != 1 {
		t.Fatalf("write_file executions = %d, want 1 (only the post-failure turn ran it)", got)
	}
}

// TestPlanWindowWaivesLockedPredicate pins the waiver predicate directly:
// previewable writers are waived; bash, unknown tools, read-only tools (an
// explicit ask rule naming one must keep prompting), and a controller without
// a registry are not.
func TestPlanWindowWaivesLockedPredicate(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakePreviewableTool{&fakeTool{name: "write_file", readOnly: false}})
	reg.Add(&fakeTool{name: "bash", readOnly: false})
	reg.Add(&fakeTool{name: "read_file", readOnly: true})

	c := New(Options{Registry: reg})
	cases := []struct {
		tool string
		want bool
	}{
		{"write_file", true},
		{"bash", false},      // no Previewer: unknowable side effects
		{"read_file", false}, // reaching the approver means an explicit ask rule — honour it
		{"mcp__server__deploy", false}, // not registered here; never waived
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, tc := range cases {
		if got := c.planWindowWaivesLocked(tc.tool); got != tc.want {
			t.Errorf("planWindowWaivesLocked(%q) = %v, want %v", tc.tool, got, tc.want)
		}
	}

	noReg := New(Options{})
	noReg.mu.Lock()
	defer noReg.mu.Unlock()
	if noReg.planWindowWaivesLocked("write_file") {
		t.Error("with no registry nothing may be waived (conservative direction)")
	}
}
