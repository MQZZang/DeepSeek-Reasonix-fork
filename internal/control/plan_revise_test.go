package control

// PR5 tests: the three-way plan approval (approve / request changes / reject).
// RevisePlan hands the user's feedback back to the model, which revises and
// resubmits within the same turn for a fresh gate; approval opens a labeled
// "plan-approved: <title>" checkpoint right before execution starts.
// Companion baselines: B4 (baseline_plan_test.go) pins plain rejection.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// planSubmitArgsV2 is the revised submission the model sends after feedback.
const planSubmitArgsV2 = `{"title":"Fix the add bug with a shim","phases":[{"name":"Add the shim","steps":["wrap the operator"]},{"name":"Verify","steps":["run go test"]}]}`

// reviseFlowController is submitFlowController with an action-sequence
// responder: the Nth pending approval is answered by actions[N] (RevisePlan,
// Approve, …), so a test can revise first and approve second.
func reviseFlowController(t *testing.T, prov *scriptedTurns, workspaceRoot, sessionPath string, actions ...func(c *Controller, id string)) (*Controller, *agent.Agent, *baselineEvents, func() []event.Approval) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(&fakeTool{name: "submit_plan", readOnly: true})
	b, sink := newBaselineEvents()
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, sink)
	c := New(Options{Runner: ag, Executor: ag, Sink: sink, WorkspaceRoot: workspaceRoot, SessionPath: sessionPath})

	var mu sync.Mutex
	var seen []event.Approval
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case a := <-b.approvals:
				mu.Lock()
				seen = append(seen, a)
				n := len(seen) - 1
				mu.Unlock()
				if n < len(actions) {
					actions[n](c, a.ID)
				} else {
					c.Approve(a.ID, false, false, false)
				}
			case <-done:
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(done)
		<-finished
	})

	c.SetPlanMode(true)
	return c, ag, b, func() []event.Approval {
		mu.Lock()
		defer mu.Unlock()
		out := make([]event.Approval, len(seen))
		copy(out, seen)
		return out
	}
}

// TestPlanRevisionFeedbackLoopResubmitsAndGates is the golden revision path:
// submit → "request changes" with feedback → the model revises and resubmits
// within the same turn → a second gate → approve → execute. The feedback rides
// in as a synthetic user message carrying the user's text verbatim; the first
// artifact ends rejected, the revised one executed.
func TestPlanRevisionFeedbackLoopResubmitsAndGates(t *testing.T) {
	root := t.TempDir()
	feedback := "Use a shim instead of editing the operator"
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "submit_plan", planSubmitArgs),
		textTurn("Plan v1 submitted."),
		toolCallTurn("c2", "submit_plan", planSubmitArgsV2),
		textTurn("Revised plan submitted."),
		textTurn("Done — shim in place."),
	}}
	c, ag, _, requested := reviseFlowController(t, prov, root, "",
		func(c *Controller, id string) { c.RevisePlan(id, feedback) },
		func(c *Controller, id string) { c.Approve(id, true, false, false) },
	)

	runTurnOK(t, c, "plan the add-bug fix")

	if got := approvalsForTool(requested(), planApprovalTool); got != 2 {
		t.Fatalf("plan approvals = %d, want 2 (original + revised)", got)
	}
	if prov.call != 5 {
		t.Fatalf("provider calls = %d, want 5 (submit + ack + resubmit + ack + execution)", prov.call)
	}
	if c.PlanMode() {
		t.Fatal("plan mode should be off after the revised plan was approved")
	}

	// The feedback rode in as a synthetic user message with the text verbatim.
	var sawFeedback bool
	for _, m := range ag.Session().Messages {
		if m.Role == provider.RoleUser && strings.HasPrefix(m.Content, planReviseMessage) && strings.Contains(m.Content, feedback) {
			sawFeedback = true
		}
	}
	if !sawFeedback {
		t.Fatal("revision feedback user message not found in the session")
	}
	if !IsSyntheticUserMessage(planReviseMessage + feedback) {
		t.Fatal("planReviseMessage must be classified synthetic so UIs hide the wrapper")
	}

	// Artifact history: v1 rejected, v2 executed — two files, no mutation.
	files, err := filepath.Glob(filepath.Join(root, ".reasonix", "plans", "*.md"))
	if err != nil || len(files) != 2 {
		t.Fatalf("plan artifacts = %v (err %v), want exactly 2", files, err)
	}
	byStatus := map[string]string{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, status := range []string{planStatusRejected, planStatusExecuted} {
			if strings.Contains(string(data), "status: "+status) {
				byStatus[status] = string(data)
			}
		}
	}
	if !strings.Contains(byStatus[planStatusRejected], "title: Fix the add bug\n") {
		t.Fatalf("rejected artifact should be plan v1, got:\n%s", byStatus[planStatusRejected])
	}
	if !strings.Contains(byStatus[planStatusExecuted], "Add the shim") {
		t.Fatalf("executed artifact should be revised plan v2, got:\n%s", byStatus[planStatusExecuted])
	}
}

// TestPlanRevisionWithoutResubmissionEndsTurn: when the model answers the
// feedback with prose instead of a new submit_plan call, the turn ends without
// a second gate — no deadlock, plan mode stays on, the artifact stays rejected.
func TestPlanRevisionWithoutResubmissionEndsTurn(t *testing.T) {
	root := t.TempDir()
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "submit_plan", planSubmitArgs),
		textTurn("Plan v1 submitted."),
		textTurn("I think the original approach is right; happy to discuss the shim idea further."),
	}}
	c, _, _, requested := reviseFlowController(t, prov, root, "",
		func(c *Controller, id string) { c.RevisePlan(id, "use a shim") },
	)

	runTurnOK(t, c, "plan the add-bug fix")

	if got := approvalsForTool(requested(), planApprovalTool); got != 1 {
		t.Fatalf("plan approvals = %d, want 1 (no resubmission, no second gate)", got)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want 3 (submit + ack + revision reply)", prov.call)
	}
	if !c.PlanMode() {
		t.Fatal("plan mode must stay on when the revision was not resubmitted")
	}
	files, _ := filepath.Glob(filepath.Join(root, ".reasonix", "plans", "*.md"))
	if len(files) != 1 {
		t.Fatalf("plan artifacts = %v, want exactly 1", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "status: rejected") {
		t.Fatalf("artifact should stay rejected:\n%s", data)
	}
}

// TestPlanApprovalCreatesLabeledCheckpoint: approving a plan opens a fresh
// checkpoint labeled "plan-approved: <title>" at the approve/execute boundary,
// so the rewind picker offers "back to just before the work started" as one
// visible jump.
func TestPlanApprovalCreatesLabeledCheckpoint(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("c1", "submit_plan", planSubmitArgs),
		textTurn("Plan submitted."),
		textTurn("Done."),
	}}
	c, _, _, _ := reviseFlowController(t, prov, t.TempDir(), sessionPath,
		func(c *Controller, id string) { c.Approve(id, true, false, false) },
	)

	runTurnOK(t, c, "plan the add-bug fix")

	metas := c.Checkpoints()
	labeled := -1
	turnCkpt := -1
	for i, m := range metas {
		if m.Prompt == planApprovedCheckpointPrefix+"Fix the add bug" {
			labeled = i
		}
		// The turn checkpoint's prompt is the composed input (plan marker +
		// the user's text), so match on the user's text.
		if strings.Contains(m.Prompt, "plan the add-bug fix") {
			turnCkpt = i
		}
	}
	if labeled == -1 {
		t.Fatalf("no plan-approved checkpoint in %+v", metas)
	}
	if turnCkpt == -1 || labeled <= turnCkpt {
		t.Fatalf("labeled checkpoint must come after the turn checkpoint (turn %d, labeled %d)", turnCkpt, labeled)
	}
}

// TestRevisePlanReplyPlumbing pins the channel contract: RevisePlan answers a
// pending plan approval with deny + feedback; on a non-plan approval the
// feedback is dropped (plain deny); an unknown ID is a silent no-op.
func TestRevisePlanReplyPlumbing(t *testing.T) {
	c := New(Options{Sink: event.Discard})

	answer := func(tool string, respond func(id string)) approvalReply {
		t.Helper()
		got := make(chan approvalReply, 1)
		ready := make(chan event.Approval, 1)
		c.sink = event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				ready <- e.Approval
			}
		})
		go func() {
			r, err := c.requestApprovalReply(context.Background(), tool, "", nil)
			if err != nil {
				t.Errorf("requestApprovalReply(%s): %v", tool, err)
			}
			got <- r
		}()
		select {
		case a := <-ready:
			respond(a.ID)
		case <-time.After(2 * time.Second):
			t.Fatalf("no ApprovalRequest for %s", tool)
		}
		select {
		case r := <-got:
			return r
		case <-time.After(2 * time.Second):
			t.Fatalf("requestApprovalReply(%s) did not return", tool)
			return approvalReply{}
		}
	}

	r := answer(planApprovalTool, func(id string) { c.RevisePlan(id, "  tighten phase 2  ") })
	if r.allow || strings.TrimSpace(r.feedback) != "tighten phase 2" {
		t.Fatalf("plan revise reply = %+v, want deny with feedback", r)
	}

	r = answer("bash", func(id string) { c.RevisePlan(id, "feedback") })
	if r.allow || r.feedback != "" {
		t.Fatalf("non-plan revise reply = %+v, want plain deny without feedback", r)
	}

	c.RevisePlan("does-not-exist", "x") // must not panic or block
}
