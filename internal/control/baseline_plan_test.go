package control

// Gate-2 baseline cases B2–B5: the Plan scenario. B1 (golden path through
// approval into execution) is already pinned by TestAutoPlanGateEndToEnd and
// plan_seed_test.go; this file adds the harness-ceiling contract, the
// diagnostics gap, the reject-iterate loop, and the auto-plan trigger corpus.
// See baseline_helpers_test.go for suite conventions.

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// planAgent builds an agent whose registry holds the given fakes, with the
// controller already in plan mode and an auto-denying plan-gate responder.
func planAgent(t *testing.T, prov *scriptedTurns, fakes ...*fakeTool) (*Controller, *agent.Agent, *baselineEvents, func() []event.Approval) {
	t.Helper()
	reg := tool.NewRegistry()
	for _, f := range fakes {
		reg.Add(f)
	}
	// The agent and the controller share one sink, as boot wires them in
	// production: ToolDispatch/ToolResult come from the agent, ApprovalRequest
	// from the controller.
	b, sink := newBaselineEvents()
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, sink)
	c := New(Options{Runner: ag, Executor: ag, Sink: sink})
	requested := autoRespondApprovals(t, c, b, func(event.Approval) bool { return false })
	c.SetPlanMode(true)
	return c, ag, b, requested
}

// TestBaselineB2PlanBlocksWriter — Gate-2 case B2 (permanent contract).
//
// In plan mode the harness refuses every ReadOnly()==false tool before the
// permission gate (agent.go executeOne). The writer must not run, the result
// fed back to the model must carry the canonical blocked notice, and the turn
// must still complete normally.
func TestBaselineB2PlanBlocksWriter(t *testing.T) {
	writer := &fakeTool{name: "write_file", readOnly: false}
	// Non-list reply: the harness-ceiling contract under test is independent of
	// the submit_plan nudge, so the reply deliberately isn't plan-shaped.
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "write_file", `{"path":"main.go"}`),
		textTurn("The writer was refused; research continues before any plan is submitted."),
	}}
	c, _, b, _ := planAgent(t, prov, writer)

	runTurnOK(t, c, "fix the add bug and add a regression test")

	if got := writer.callCount(); got != 0 {
		t.Fatalf("writer executed %d times in plan mode, want 0", got)
	}
	results := resultsForTool(drainToolResults(b), "write_file")
	if len(results) != 1 {
		t.Fatalf("write_file tool results = %d, want 1 blocked result", len(results))
	}
	// Wording generalized in PR2 when ask mode became the second read-only
	// ceiling consumer: the blocked notice is mode-neutral, the turn-tail
	// marker carries the mode-specific guidance. Still pinned byte-for-byte.
	if results[0].Err != "blocked: read-only mode" {
		t.Fatalf("blocked errMsg = %q, want %q", results[0].Err, "blocked: read-only mode")
	}
	if !strings.Contains(results[0].Output, "the current mode is read-only") {
		t.Fatalf("blocked output fed to model = %q, want the read-only guidance", results[0].Output)
	}
	if !c.PlanMode() {
		t.Fatal("a research turn without a submission must keep plan mode on")
	}
}

// TestBaselineB3ReadOnlyBashAndTaskAllowedUnderPlanMode — Gate-2 case B3
// (resolved in PR3).
//
// Under CeilingReadOnly a tool whose static ReadOnly() is false is normally
// blocked. PR3 introduces the commandReadOnly optional interface: a tool may
// implement IsReadOnlyCommand(args) to report per-invocation read-only status.
//
//   - bash + IsReadOnlyCommand returning true (simulates "git log -5")  → admitted
//   - task + IsReadOnlyCommand returning true (ceiling-inheriting)       → admitted
//
// This test also verifies that the PlanModeMarker accurately advertises bash
// (read-only) and task as available — fixing the documented prompt-vs-harness
// contradiction that the KNOWN-GAP had pinned.
func TestBaselineB3ReadOnlyBashAndTaskAllowedUnderPlanMode(t *testing.T) {
	// bash: IsReadOnlyCommand returns true for "git log …" (read-only command)
	bash := &fakeTool{
		name:     "bash",
		readOnly: false,
		commandClassifier: func(args json.RawMessage) bool {
			var p struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal(args, &p)
			return strings.Contains(p.Command, "git log")
		},
	}
	// task: IsReadOnlyCommand returns true — the sub-agent inherits ceiling
	task := &fakeTool{
		name:              "task",
		readOnly:          false,
		commandClassifier: func(json.RawMessage) bool { return true },
	}
	// Non-list reply: keeps the submit_plan nudge out of this ceiling test.
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "bash", `{"command":"git log -5"}`),
		toolCallTurn("c2", "task", `{"prompt":"survey the failing tests"}`),
		textTurn("Research done; drafting the plan next."),
	}}
	c, _, b, _ := planAgent(t, prov, bash, task)

	runTurnOK(t, c, "run the tests first, then plan the fix")

	if bash.callCount() != 1 || task.callCount() != 1 {
		t.Fatalf("plan mode bash=%d task=%d, want 1/1 (read-only bash and ceiling-inheriting task are admitted)",
			bash.callCount(), task.callCount())
	}
	results := drainToolResults(b)
	for _, name := range []string{"bash", "task"} {
		rs := resultsForTool(results, name)
		if len(rs) != 1 || rs[0].Err != "" {
			t.Fatalf("%s in plan mode: results=%+v, want exactly one successful result", name, rs)
		}
	}
	// The PlanModeMarker now accurately claims bash (read-only), task, and ask
	// are available — the prompt-vs-harness contradiction is resolved. (All
	// three are admitted under the read-only ceiling: bash via the
	// per-invocation classifier, task via ceiling inheritance, ask is
	// statically read-only.)
	if !strings.Contains(PlanModeMarker, "task, ask are available") {
		t.Fatal("PlanModeMarker must still claim task and ask are available (accurate post-PR3)")
	}
	if !strings.Contains(PlanModeMarker, "bash (read-only commands") {
		t.Fatal("PlanModeMarker must advertise bash read-only diagnostics (accurate post-PR3)")
	}
	if !c.PlanMode() {
		t.Fatal("a research turn without a submission must keep plan mode on")
	}
}

// TestBaselineB3WriterBashStillBlockedUnderPlanMode — Gate-2 case B3 companion.
//
// Even after PR3, bash with a side-effecting command (no commandClassifier, or
// classifier returning false) is still refused by the CeilingReadOnly harness.
// This test pins the static-writer contract that B2 establishes for write_file,
// but for the bash tool specifically.
func TestBaselineB3WriterBashStillBlockedUnderPlanMode(t *testing.T) {
	// A plain fakeTool without commandClassifier: IsReadOnlyCommand → false.
	bash := &fakeTool{name: "bash", readOnly: false}
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "bash", `{"command":"rm -rf output"}`),
		textTurn("The delete was refused; no plan submitted yet."),
	}}
	c, _, b, _ := planAgent(t, prov, bash)

	runTurnOK(t, c, "delete the output folder, then plan the cleanup")

	if bash.callCount() != 0 {
		t.Fatalf("writer bash executed %d times in plan mode, want 0", bash.callCount())
	}
	results := resultsForTool(drainToolResults(b), "bash")
	if len(results) != 1 || results[0].Err != "blocked: read-only mode" {
		t.Fatalf("writer bash: results=%+v, want one blocked result with 'blocked: read-only mode'", results)
	}
	_ = c
}

// TestBaselineB4PlanRejectKeepsPlanModeAndIterates — Gate-2 case B4.
//
// Rejecting a submitted plan keeps plan mode on, and the next user turn still
// rides the PlanModeMarker so the revision loop stays read-only. The second
// submission raises the gate again. (Rewritten in PR4 for the submit_plan
// flow: each proposal is an explicit tool call followed by the text reply.)
func TestBaselineB4PlanRejectKeepsPlanModeAndIterates(t *testing.T) {
	submit := &fakeTool{name: "submit_plan", readOnly: true}
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "submit_plan", `{"title":"Add CopyFile","phases":[{"name":"Phase A","steps":["write the helper"]}]}`),
		textTurn("Plan:\n1. Phase A\n   - write the helper"),
		toolCallTurn("c2", "submit_plan", `{"title":"Add CopyFile","phases":[{"name":"Phase A","steps":["table-driven test instead"]}]}`),
		textTurn("Plan v2:\n1. Phase A\n   - table-driven test instead"),
	}}
	c, ag, _, requested := planAgent(t, prov, submit)

	runTurnOK(t, c, "add CopyFile with tests")
	if !c.PlanMode() {
		t.Fatal("first rejection must keep plan mode on")
	}

	runTurnOK(t, c, "make step 2 a table-driven test")

	msgs := ag.Session().Messages
	var lastUser string
	for _, m := range msgs {
		if m.Role == provider.RoleUser {
			lastUser = m.Content
		}
	}
	if !strings.HasPrefix(lastUser, PlanModeMarker) {
		t.Fatalf("revision turn user message = %.80q…, want PlanModeMarker prefix", lastUser)
	}
	if got := approvalsForTool(requested(), planApprovalTool); got != 2 {
		t.Fatalf("plan approvals across two submissions = %d, want 2", got)
	}
	if !c.PlanMode() {
		t.Fatal("second rejection must keep plan mode on")
	}
	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want 4 (two submit+reply rounds, no execution turns after rejections)", prov.call)
	}
}

// TestBaselineB5AutoPlanCorpus — Gate-2 case B5.
//
// A labelled corpus pinning autoPlanScore's current trigger decisions
// (threshold: score >= 2 enters plan mode when no classifier is configured,
// auto_plan.go shouldAutoPlan). Rows marked gap document misfires.
//
// KNOWN-GAP(risk-8 → PR10): the two gap rows are single-file small edits that
// the keyword heuristic force-switches into plan mode; PR10 retunes the
// heuristic/classifier so they score below the threshold, then flips wantPlan.
func TestBaselineB5AutoPlanCorpus(t *testing.T) {
	corpus := []struct {
		input    string
		wantPlan bool
		gap      bool
	}{
		// Should NOT trigger plan mode.
		{"解释一下这个函数的作用", false, false},
		{"what does Compose do in input.go?", false, false},
		{"run go test ./internal/control and show the output", false, false},
		{"把变量名 x 改成 count", false, false},
		{"fix the typo in the README title", false, false},
		{"修复这个问题：按钮文案有个错别字", false, false},
		// Misfires: the same one-line-fix intent as the row above flips to a
		// forced plan switch when a multi-surface keyword ("配置"/"config")
		// happens to appear — keyword fragility, not task complexity.
		{"修复这个问题：配置里有个错别字", true, true},      // KNOWN-GAP(risk-8 → PR10)
		{"fix the issue in the config parser", true, true}, // KNOWN-GAP(risk-8 → PR10)
		// Should trigger plan mode.
		{"实现 X 功能，前端后端都要改，并补测试和文档", true, false},
		{"refactor the session persistence layer across multiple files, update tests and docs", true, false},
		{"按这份 PRD 实现需求：\n1. 新增配置项\n2. 接入 boot\n3. 补集成测试", true, false},
	}
	for _, row := range corpus {
		got := autoPlanScore(row.input) >= 2
		if got != row.wantPlan {
			t.Errorf("autoPlanScore(%q) >= 2 = %v, want %v (gap=%v)", row.input, got, row.wantPlan, row.gap)
		}
	}
}
