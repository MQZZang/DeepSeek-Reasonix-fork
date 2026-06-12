package control

// Gate-2 baseline case G1: the prefix-cache architecture invariant.
//
// PERMANENT GATE — this test must never be flipped or weakened. Every mode
// instruction rides the turn tail (control.Compose), never the system message
// or the tool schemas, so DeepSeek's prefix cache stays warm across mode
// switches (REASONIX.md, input.go PlanModeMarker doc). Any mode-system PR that
// turns this red is wrong by definition.

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestBaselineG1PrefixStableAcrossModeToggles toggles normal → plan → ask →
// goal → yolo → normal across six turns on one session and asserts the
// cache-stable prefix is byte-identical throughout: the system message never
// changes, no extra system message is ever inserted, and the tool schemas
// never change. (PR2 extended the cycle with ask mode — strictly more
// assertions, never fewer.)
func TestBaselineG1PrefixStableAcrossModeToggles(t *testing.T) {
	const systemPrompt = "You are the baseline system prompt. Stay byte-stable."

	reader := &fakeTool{name: "read_file", readOnly: true}
	writer := &fakeTool{name: "write_file", readOnly: false}
	reg := tool.NewRegistry()
	reg.Add(reader)
	reg.Add(writer)

	schemaBytes := func() string {
		b, err := json.Marshal(reg.Schemas())
		if err != nil {
			t.Fatalf("marshal schemas: %v", err)
		}
		return string(b)
	}

	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("turn 1 (normal)"),
		textTurn("turn 2 (plan)"),
		textTurn("turn 3 (ask)"),
		textTurn("turn 4 (goal)\n\n[goal:complete]"),
		textTurn("turn 5 (yolo)"),
		textTurn("turn 6 (normal again)"),
	}}
	ag := agent.New(prov, reg, agent.NewSession(systemPrompt), agent.Options{}, event.Discard)

	b, sink := newBaselineEvents()
	c := New(Options{Runner: ag, Executor: ag, Sink: sink})
	// Plan proposals are denied so the plan turn never enters execution; the
	// invariant must hold on the research turn alone.
	autoRespondApprovals(t, c, b, func(event.Approval) bool { return false })

	wantSchemas := schemaBytes()
	assertPrefixStable := func(step string) {
		t.Helper()
		msgs := ag.Session().Snapshot()
		if len(msgs) == 0 || msgs[0].Role != provider.RoleSystem {
			t.Fatalf("%s: session must keep the system message at index 0", step)
		}
		if msgs[0].Content != systemPrompt {
			t.Fatalf("%s: system message changed:\n got %q\nwant %q", step, msgs[0].Content, systemPrompt)
		}
		for i, m := range msgs[1:] {
			if m.Role == provider.RoleSystem {
				t.Fatalf("%s: extra system message inserted at %d: %q", step, i+1, m.Content)
			}
		}
		if got := schemaBytes(); got != wantSchemas {
			t.Fatalf("%s: tool schemas changed:\n got %s\nwant %s", step, got, wantSchemas)
		}
	}

	steps := []struct {
		name  string
		setup func()
		input string
	}{
		{"normal", func() {}, "first question"},
		{"plan", func() { c.SetPlanMode(true) }, "draft a plan"},
		{"ask", func() { c.SetCollaborationMode(CollabAsk) }, "just answer this"},
		{"goal", func() { c.SetCollaborationMode(CollabNormal); c.SetGoal("finish the feature") }, "work the goal"},
		{"yolo", func() { c.SetToolApprovalMode(ToolApprovalYolo) }, "now with yolo"},
		{"normal-again", func() { c.SetToolApprovalMode(ToolApprovalAsk) }, "back to normal"},
	}
	for _, s := range steps {
		s.setup()
		runTurnOK(t, c, s.input)
		assertPrefixStable(s.name)
	}

	// The mode framing must have landed in user turns, not the prefix: the plan
	// and ask turns' user messages carry their markers, and the goal turn the
	// goal block.
	var sawPlanMarker, sawAskMarker, sawGoalBlock bool
	for _, m := range ag.Session().Snapshot() {
		if m.Role != provider.RoleUser {
			continue
		}
		if strings.HasPrefix(m.Content, PlanModeMarker) {
			sawPlanMarker = true
		}
		if strings.HasPrefix(m.Content, AskModeMarker) {
			sawAskMarker = true
		}
		if strings.Contains(m.Content, activeGoalOpen) {
			sawGoalBlock = true
		}
	}
	if !sawPlanMarker {
		t.Fatal("plan turn should carry PlanModeMarker in its user message (turn-tail injection)")
	}
	if !sawAskMarker {
		t.Fatal("ask turn should carry AskModeMarker in its user message (turn-tail injection)")
	}
	if !sawGoalBlock {
		t.Fatal("goal turn should carry the <active-goal> block in its user message (turn-tail injection)")
	}
}
