package control

// PR4 tests: the submit_plan submission contract, the plan artifact
// lifecycle (.reasonix/plans/<id>.md, draft → approved|rejected → executed),
// the structured todo seeding, and the one-shot nudge for plan-shaped replies
// that skipped submit_plan. Companion baselines: A3 (baseline_ask_test.go),
// B4 (baseline_plan_test.go), golden path (auto_plan_e2e_test.go).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestParsePlanSubmission(t *testing.T) {
	cases := []struct {
		name string
		args string
		ok   bool
	}{
		{"valid", `{"title":"T","phases":[{"name":"A","steps":["s1"]}]}`, true},
		{"valid no steps", `{"title":"T","phases":[{"name":"A"}]}`, true},
		{"missing title", `{"phases":[{"name":"A"}]}`, false},
		{"blank title", `{"title":"  ","phases":[{"name":"A"}]}`, false},
		{"no phases", `{"title":"T","phases":[]}`, false},
		{"blank phase name", `{"title":"T","phases":[{"name":"  "}]}`, false},
		{"malformed json", `{"title":`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := parsePlanSubmission(tc.args); ok != tc.ok {
				t.Fatalf("parsePlanSubmission(%s) ok = %v, want %v", tc.args, ok, tc.ok)
			}
		})
	}
}

// TestPlanTodosArgsFromSubmission pins the structured seeding shape (B1):
// phases at level 0, steps at level 1, first item in_progress, blanks
// skipped, capped at 20 like the markdown parser.
func TestPlanTodosArgsFromSubmission(t *testing.T) {
	args := planTodosArgsFromSubmission(PlanSubmission{
		Title: "T",
		Phases: []PlanPhase{
			{Name: "Phase A", Steps: []string{"step one", "  ", "step two"}},
			{Name: "Phase B"},
		},
	})
	var p struct {
		Todos []seedTodo `json:"todos"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		t.Fatalf("unmarshal seeded args: %v (%s)", err, args)
	}
	want := []seedTodo{
		{Content: "Phase A", Status: "in_progress", Level: 0},
		{Content: "step one", Status: "pending", Level: 1},
		{Content: "step two", Status: "pending", Level: 1},
		{Content: "Phase B", Status: "pending", Level: 0},
	}
	if len(p.Todos) != len(want) {
		t.Fatalf("todos = %+v, want %d items", p.Todos, len(want))
	}
	for i, w := range want {
		if p.Todos[i] != w {
			t.Fatalf("todo[%d] = %+v, want %+v", i, p.Todos[i], w)
		}
	}

	// Cap: a 30-phase submission seeds at most 20 items.
	big := PlanSubmission{Title: "T"}
	for i := 0; i < 30; i++ {
		big.Phases = append(big.Phases, PlanPhase{Name: "P"})
	}
	var capped struct {
		Todos []seedTodo `json:"todos"`
	}
	if err := json.Unmarshal([]byte(planTodosArgsFromSubmission(big)), &capped); err != nil {
		t.Fatalf("unmarshal capped args: %v", err)
	}
	if len(capped.Todos) != 20 {
		t.Fatalf("capped todos = %d, want 20", len(capped.Todos))
	}
}

func TestRenderPlanMarkdown(t *testing.T) {
	got := renderPlanMarkdown(PlanSubmission{
		Title: "T",
		Phases: []PlanPhase{
			{Name: "Phase A", Steps: []string{"step one"}},
			{Name: "Phase B"},
		},
	})
	want := "1. Phase A\n   - step one\n2. Phase B\n"
	if got != want {
		t.Fatalf("renderPlanMarkdown = %q, want %q", got, want)
	}
	// The rendered body round-trips through the legacy markdown parser.
	if items := parsePlanTodos(got); len(items) != 3 {
		t.Fatalf("rendered markdown parsed to %d todos, want 3", len(items))
	}
}

func TestPlanSlug(t *testing.T) {
	cases := map[string]string{
		"Fix the add bug":    "fix-the-add-bug",
		"  Weird --- Title!": "weird-title",
		"修复加法缺陷":             "plan", // non-ASCII-only titles fall back
		"A1 b2":              "a1-b2",
	}
	for in, want := range cases {
		if got := planSlug(in); got != want {
			t.Fatalf("planSlug(%q) = %q, want %q", in, got, want)
		}
	}
	long := planSlug(strings.Repeat("abc ", 30))
	if len(long) > 41 { // 40 + a possible trailing rune trimmed below
		t.Fatalf("planSlug long title = %q (%d bytes), want capped near 40", long, len(long))
	}
	if id := planArtifactID(time.Date(2026, 6, 11, 12, 7, 5, 0, time.UTC), "Fix It"); id != "20260611-120705-fix-it" {
		t.Fatalf("planArtifactID = %q", id)
	}
}

func TestSetPlanArtifactStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	body := "---\nid: x\ntitle: T\nstatus: draft\ncreated_at: now\n---\n\n# T\n\nstatus: draft should not change in the body\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	setPlanArtifactStatus(path, planStatusApproved)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "status: approved\n") {
		t.Fatalf("frontmatter status not updated: %s", got)
	}
	if !strings.Contains(string(got), "status: draft should not change in the body") {
		t.Fatalf("body text was touched: %s", got)
	}
	// Robustness: a "" path (artifact never written) is a silent no-op.
	setPlanArtifactStatus("", planStatusApproved)
}

// planSubmitArgs is the canonical two-phase submission used by the e2e tests.
const planSubmitArgs = `{"title":"Fix the add bug","phases":[{"name":"Reproduce","steps":["write failing test"]},{"name":"Fix","steps":["correct the operator"]}]}`

// submitFlowController wires a real agent + controller around scripted turns
// with a registered fake submit_plan tool, in plan mode, with an approval
// responder driven by decide.
func submitFlowController(t *testing.T, prov *scriptedTurns, workspaceRoot string, decide func(event.Approval) bool) (*Controller, *agent.Agent, *baselineEvents, func() []event.Approval) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(&fakeTool{name: "submit_plan", readOnly: true})
	b, sink := newBaselineEvents()
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, sink)
	c := New(Options{Runner: ag, Executor: ag, Sink: sink, WorkspaceRoot: workspaceRoot})
	requested := autoRespondApprovals(t, c, b, decide)
	c.SetPlanMode(true)
	return c, ag, b, requested
}

// TestPlanNudgeRecoversMissedSubmission: a plan-shaped text reply without a
// submit_plan call gets exactly one synthetic nudge; when the model answers
// the nudge with a submission, the gate fires as if it had submitted upfront.
func TestPlanNudgeRecoversMissedSubmission(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Plan:\n1. Reproduce\n   - write failing test\n2. Fix"),
		toolCallTurn("c1", "submit_plan", planSubmitArgs),
		textTurn("Submitted the plan."),
	}}
	c, ag, _, requested := submitFlowController(t, prov, "", func(event.Approval) bool { return false })

	runTurnOK(t, c, "plan the add-bug fix")

	if got := approvalsForTool(requested(), planApprovalTool); got != 1 {
		t.Fatalf("approvals after nudge recovery = %d, want 1", got)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want 3 (reply + nudged submit + ack)", prov.call)
	}
	// The nudge rode in as a synthetic user message UIs must not render.
	var sawNudge bool
	for _, m := range ag.Session().Messages {
		if m.Role == provider.RoleUser && strings.HasPrefix(m.Content, planSubmitNudgeMessage) {
			sawNudge = true
		}
	}
	if !sawNudge {
		t.Fatal("nudge user message not found in the session")
	}
	if !IsSyntheticUserMessage(planSubmitNudgeMessage) {
		t.Fatal("planSubmitNudgeMessage must be classified synthetic so UIs hide it")
	}
	if !c.PlanMode() {
		t.Fatal("denying the recovered gate must keep plan mode on")
	}
}

// TestPlanNudgeDeclinedEndsTurnWithoutGate: when the model answers the nudge
// without submitting (its text was an answer, not a plan), the turn ends with
// no gate, a notice tells the user, and plan mode stays on.
func TestPlanNudgeDeclinedEndsTurnWithoutGate(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("The stop conditions are:\n1. no tool calls\n2. readiness gate passes"),
		textTurn("That was an explanation of the loop, not a plan."),
	}}
	c, _, b, requested := submitFlowController(t, prov, "", func(event.Approval) bool { return true })

	runTurnOK(t, c, "explain the agent loop stop conditions")

	if got := len(requested()); got != 0 {
		t.Fatalf("approvals after declined nudge = %d, want 0", got)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2 (reply + declined nudge)", prov.call)
	}
	if !containsSubstring(drainNotices(b), i18n.M.PlanNudgeDeclined) {
		t.Fatal("declined nudge must emit the PlanNudgeDeclined notice")
	}
	if !c.PlanMode() {
		t.Fatal("a declined nudge must keep plan mode on")
	}
}

// TestPlanArtifactLifecycleApproved: a submission persists a draft artifact
// under .reasonix/plans/, approval moves it through approved to executed once
// the execution turn completes, and the artifact carries the structured plan.
func TestPlanArtifactLifecycleApproved(t *testing.T) {
	root := t.TempDir()
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "submit_plan", planSubmitArgs),
		textTurn("Plan:\n1. Reproduce\n   - write failing test\n2. Fix\n   - correct the operator"),
		textTurn("Done — fixed."),
	}}
	c, _, b, _ := submitFlowController(t, prov, root, func(event.Approval) bool { return true })

	runTurnOK(t, c, "plan and fix the add bug")

	if c.PlanMode() {
		t.Fatal("plan mode should be off after approval")
	}
	files, err := filepath.Glob(filepath.Join(root, ".reasonix", "plans", "*.md"))
	if err != nil || len(files) != 1 {
		t.Fatalf("plan artifacts = %v (err %v), want exactly 1", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"status: executed",
		"title: Fix the add bug",
		"# Fix the add bug",
		"1. Reproduce",
		"   - write failing test",
		"2. Fix",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("artifact missing %q:\n%s", want, content)
		}
	}
	if !containsSubstring(drainNotices(b), filepath.Base(files[0])) {
		t.Fatal("artifact path notice not emitted")
	}
}

// TestPlanArtifactLifecycleRejected: a rejected submission leaves a rejected
// artifact on disk and plan mode on; a revised submission writes a NEW
// artifact rather than mutating the first.
func TestPlanArtifactLifecycleRejected(t *testing.T) {
	root := t.TempDir()
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "submit_plan", planSubmitArgs),
		textTurn("Plan:\n1. Reproduce\n2. Fix"),
	}}
	c, _, _, _ := submitFlowController(t, prov, root, func(event.Approval) bool { return false })

	runTurnOK(t, c, "plan the add-bug fix")

	if !c.PlanMode() {
		t.Fatal("rejection must keep plan mode on")
	}
	files, err := filepath.Glob(filepath.Join(root, ".reasonix", "plans", "*.md"))
	if err != nil || len(files) != 1 {
		t.Fatalf("plan artifacts = %v (err %v), want exactly 1", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "status: rejected") {
		t.Fatalf("artifact status not rejected:\n%s", data)
	}
}

// TestPlanSubmissionOutsidePlanModeIgnored: submit_plan in normal mode is a
// recorded no-op for the controller — no gate, no artifact. The structured
// note still lands in the session, but nothing stateful happens.
func TestPlanSubmissionOutsidePlanModeIgnored(t *testing.T) {
	root := t.TempDir()
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "submit_plan", planSubmitArgs),
		textTurn("Noted the plan."),
	}}
	reg := tool.NewRegistry()
	reg.Add(&fakeTool{name: "submit_plan", readOnly: true})
	b, sink := newBaselineEvents()
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, sink)
	c := New(Options{Runner: ag, Executor: ag, Sink: sink, WorkspaceRoot: root})
	requested := autoRespondApprovals(t, c, b, func(event.Approval) bool { return true })

	runTurnOK(t, c, "jot this down as a plan")

	if got := len(requested()); got != 0 {
		t.Fatalf("approvals in normal mode = %d, want 0", got)
	}
	if files, _ := filepath.Glob(filepath.Join(root, ".reasonix", "plans", "*.md")); len(files) != 0 {
		t.Fatalf("plan artifacts in normal mode = %v, want none", files)
	}
}
