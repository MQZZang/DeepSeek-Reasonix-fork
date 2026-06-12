package control

// Baseline behaviour suite (PR0 of the mode-system workstream).
//
// These tests pin the CURRENT behaviour of the collaboration/approval mode
// machinery — including behaviour that is a known product gap. Gaps are
// asserted as they are today and tagged with a grep-able marker:
//
//	// KNOWN-GAP(risk-N → PR-K): <target semantics in one line>
//
// where risk-N is the Gate-1 risk id and PR-K is the planned change that will
// flip the assertion. A KNOWN-GAP test going red therefore means the gap's
// behaviour changed — either intentionally (update the test in that PR) or by
// accident (regression). Never delete a KNOWN-GAP test; flip it.
//
// Case map (Gate-2 workflow baselines → test functions):
//
//	A1 → TestBaselineA1ReadOnlyQuestionNoApproval     (baseline_ask_test.go)
//	A2 → TestBaselineA2WriterUnderThreePostures       (baseline_ask_test.go)
//	A3 → TestBaselineA3PlanModeQADoesNotGate /
//	     TestBaselineA3PlanSubmissionRaisesGate       (baseline_ask_test.go)
//	B2 → TestBaselineB2PlanBlocksWriter               (baseline_plan_test.go)
//	B3 → TestBaselineB3ReadOnlyBashAndTaskAllowedUnderPlanMode /
//	     TestBaselineB3WriterBashStillBlockedUnderPlanMode (baseline_plan_test.go)
//	B4 → TestBaselineB4PlanRejectKeepsPlanModeAndIterates (baseline_plan_test.go)
//	B5 → TestBaselineB5AutoPlanCorpus                 (baseline_plan_test.go)
//	D2 → TestBaselineD2ErrorStormTriggersLoopGuard /
//	     TestBaselineD2DeniedApprovalsDoNotTriggerLoopGuard (baseline_agent_test.go)
//	F2 → TestBaselineF2GoalMarkerlessRepliesPauseAfterTwo (baseline_goal_test.go)
//	F4 → TestBaselineF4GoalPersistsAcrossResume        (baseline_goal_test.go)
//	G1 → TestBaselineG1PrefixStableAcrossModeToggles  (baseline_invariants_test.go)
//
// Existing tests already pin: D2-deny UX (approval_e2e_test.go), F1/F3
// (goal_test.go), B1/B4-reject (auto_plan_e2e_test.go, plan_seed_test.go),
// E3 (internal/evidence, internal/tool/builtin). This suite adds only what
// those do not cover; it deliberately contains no product-code changes.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
)

// fakeTool is a scriptable in-memory tool for driving the agent loop without
// touching the filesystem. Name and readOnly are fixed at construction; every
// successful Execute records the raw args.
//
// commandClassifier, when non-nil, implements the agent.commandReadOnly optional
// interface: the ceiling check calls IsReadOnlyCommand(args) instead of relying
// solely on ReadOnly(). This lets PR3 tests simulate bash read-only commands and
// ceiling-inheriting task calls without wiring real tools.
type fakeTool struct {
	name              string
	readOnly          bool
	execErr           error
	commandClassifier func(json.RawMessage) bool

	mu    sync.Mutex
	calls []string
}

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return "baseline fake " + f.name }
func (f *fakeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"path":{"type":"string"}}}`)
}
func (f *fakeTool) ReadOnly() bool { return f.readOnly }

// IsReadOnlyCommand satisfies the agent.commandReadOnly optional interface so
// the ceiling check can distinguish read-only invocations of a statically-writer
// tool (e.g. bash with "git log" vs "rm -rf").
func (f *fakeTool) IsReadOnlyCommand(args json.RawMessage) bool {
	if f.commandClassifier == nil {
		return false
	}
	return f.commandClassifier(args)
}

func (f *fakeTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	if f.execErr != nil {
		return "", f.execErr
	}
	f.mu.Lock()
	f.calls = append(f.calls, string(args))
	f.mu.Unlock()
	return "ok", nil
}

func (f *fakeTool) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

var errFakeBoom = errors.New("boom: simulated tool failure")

// baselineEvents fans selected event kinds into per-kind channels so tests can
// assert ordering without racing the sink goroutine.
type baselineEvents struct {
	approvals   chan event.Approval
	toolResults chan event.Tool
	notices     chan string
}

func newBaselineEvents() (*baselineEvents, event.Sink) {
	b := &baselineEvents{
		approvals:   make(chan event.Approval, 16),
		toolResults: make(chan event.Tool, 64),
		notices:     make(chan string, 64),
	}
	sink := event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.ApprovalRequest:
			b.approvals <- e.Approval
		case event.ToolResult:
			b.toolResults <- e.Tool
		case event.Notice:
			b.notices <- e.Text
		}
	})
	return b, sink
}

// autoRespondApprovals answers every ApprovalRequest with the verdict chosen by
// decide (allow once, never session/persist grants) until the test ends. It
// keeps plan-gate and tool-gate turns from deadlocking under goleak.
func autoRespondApprovals(t *testing.T, c *Controller, b *baselineEvents, decide func(event.Approval) bool) (requested func() []event.Approval) {
	t.Helper()
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
				mu.Unlock()
				c.Approve(a.ID, decide(a), false, false)
			case <-done:
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(done)
		<-finished
	})
	return func() []event.Approval {
		mu.Lock()
		defer mu.Unlock()
		out := make([]event.Approval, len(seen))
		copy(out, seen)
		return out
	}
}

// drainToolResults collects every ToolResult currently buffered.
func drainToolResults(b *baselineEvents) []event.Tool {
	var out []event.Tool
	for {
		select {
		case r := <-b.toolResults:
			out = append(out, r)
		default:
			return out
		}
	}
}

// drainNotices collects every Notice currently buffered.
func drainNotices(b *baselineEvents) []string {
	var out []string
	for {
		select {
		case n := <-b.notices:
			out = append(out, n)
		default:
			return out
		}
	}
}

func approvalsForTool(approvals []event.Approval, tool string) int {
	n := 0
	for _, a := range approvals {
		if a.Tool == tool {
			n++
		}
	}
	return n
}

func resultsForTool(results []event.Tool, name string) []event.Tool {
	var out []event.Tool
	for _, r := range results {
		if r.Name == name {
			out = append(out, r)
		}
	}
	return out
}

func runTurnOK(t *testing.T, c *Controller, input string) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.runTurnWithRaw(context.Background(), input, input) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runTurnWithRaw(%q): %v", input, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("runTurnWithRaw(%q) timed out", input)
	}
}

func containsSubstring(haystack []string, sub string) bool {
	for _, s := range haystack {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
