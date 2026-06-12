package control

// PR2 unit tests for ask mode: the /ask command family, the collaboration-mode
// axis (mutual exclusion with plan, ceiling wiring, bool shims), the
// AskModeMarker turn-tail composition, auto-plan suppression, and the goal
// interaction. The Gate-2 resolved-side contracts live in baseline_ask_test.go
// (TestBaselineA2AskModeWriterBlockedUnderAllPostures /
// TestBaselineA3AskModeQANoApprovalGate).

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestParseAskCommand(t *testing.T) {
	cases := []struct {
		input      string
		wantOK     bool
		wantAction AskCommandAction
		wantText   string
	}{
		{"/ask", true, AskCommandOn, ""},
		{"  /ask  ", true, AskCommandOn, ""},
		{"/ask on", true, AskCommandOn, ""},
		{"/ask off", true, AskCommandOff, ""},
		{"/ask exit", true, AskCommandOff, ""},
		{"/ask stop", true, AskCommandOff, ""},
		{"/ask clear", true, AskCommandOff, ""},
		{"/ask why does Compose strip markers?", true, AskCommandQuestion, "why does Compose strip markers?"},
		{"/ask\twhat is boot.Build?", true, AskCommandQuestion, "what is boot.Build?"},
		{"/asked about this", false, AskCommandOn, ""},
		{"ask something", false, AskCommandOn, ""},
		{"/goal ship it", false, AskCommandOn, ""},
	}
	for _, tc := range cases {
		cmd, ok := ParseAskCommand(tc.input)
		if ok != tc.wantOK {
			t.Errorf("ParseAskCommand(%q) ok = %v, want %v", tc.input, ok, tc.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if cmd.Action != tc.wantAction || cmd.Text != tc.wantText {
			t.Errorf("ParseAskCommand(%q) = {%v %q}, want {%v %q}",
				tc.input, cmd.Action, cmd.Text, tc.wantAction, tc.wantText)
		}
	}
}

func TestAskCommandTogglesMode(t *testing.T) {
	var notices []string
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})})

	c.Submit("/ask")
	if got := c.CollaborationMode(); got != CollabAsk {
		t.Fatalf("after /ask: CollaborationMode() = %q, want %q", got, CollabAsk)
	}
	if c.PlanMode() {
		t.Fatal("ask mode must not report plan mode")
	}

	c.Submit("/ask off")
	if got := c.CollaborationMode(); got != CollabNormal {
		t.Fatalf("after /ask off: CollaborationMode() = %q, want %q", got, CollabNormal)
	}

	joined := strings.Join(notices, "\n")
	for _, want := range []string{i18n.M.AskModeOn, i18n.M.AskModeOff} {
		if !strings.Contains(joined, want) {
			t.Fatalf("notices missing %q, got:\n%s", want, joined)
		}
	}
}

func TestAskCommandWithQuestionRunsTurnWithMarker(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Compose strips markers so the display text matches what you typed."),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.Submit("/ask why does Compose strip markers?")
	waitForTurnDone(t, events)

	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.call)
	}
	first := firstUserMessage(ag.Session().Messages)
	if !strings.HasPrefix(first, AskModeMarker) {
		t.Fatalf("ask turn should ride the AskModeMarker, got %q", first)
	}
	if !strings.Contains(first, "why does Compose strip markers?") {
		t.Fatalf("ask turn should carry the question, got %q", first)
	}
	if got := c.CollaborationMode(); got != CollabAsk {
		t.Fatalf("after the answer: CollaborationMode() = %q, want %q (mode is sticky)", got, CollabAsk)
	}
}

func TestSetCollaborationModeMutualExclusionAndCeiling(t *testing.T) {
	ag := agent.New(&scriptedTurns{}, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag})

	c.SetCollaborationMode(CollabPlan)
	if !c.PlanMode() || c.CollaborationMode() != CollabPlan {
		t.Fatalf("plan: PlanMode()=%v CollaborationMode()=%q", c.PlanMode(), c.CollaborationMode())
	}
	if got := ag.Ceiling(); got != agent.CeilingReadOnly {
		t.Fatalf("plan ceiling = %v, want CeilingReadOnly", got)
	}

	c.SetCollaborationMode(CollabAsk)
	if c.PlanMode() || c.CollaborationMode() != CollabAsk {
		t.Fatalf("ask: PlanMode()=%v CollaborationMode()=%q", c.PlanMode(), c.CollaborationMode())
	}
	if got := ag.Ceiling(); got != agent.CeilingReadOnly {
		t.Fatalf("ask ceiling = %v, want CeilingReadOnly", got)
	}

	// The bool shim only exits plan mode: SetPlanMode(false) from ask is a
	// no-op, an explicit SetMode/SetCollaborationMode clears ask.
	c.SetPlanMode(false)
	if got := c.CollaborationMode(); got != CollabAsk {
		t.Fatalf("SetPlanMode(false) from ask = %q, want ask untouched", got)
	}
	c.SetMode(false, false)
	if got := c.CollaborationMode(); got != CollabNormal {
		t.Fatalf("SetMode(false,_) from ask = %q, want normal", got)
	}
	if got := ag.Ceiling(); got != agent.CeilingFull {
		t.Fatalf("normal ceiling = %v, want CeilingFull", got)
	}

	// Normalization: junk input lands on normal, never a half state.
	c.SetCollaborationMode("ASK")
	if got := c.CollaborationMode(); got != CollabAsk {
		t.Fatalf("case-insensitive ask = %q, want ask", got)
	}
	c.SetCollaborationMode("bogus")
	if got := c.CollaborationMode(); got != CollabNormal {
		t.Fatalf("bogus mode = %q, want normal", got)
	}
}

func TestAutoPlanSuppressedInAskMode(t *testing.T) {
	// Potent input: pinned by TestBaselineB5AutoPlanCorpus as score >= 2.
	const potent = "实现 X 功能，前端后端都要改，并补测试和文档"

	c := New(Options{AutoPlan: "on"})
	if !c.shouldAutoPlan(context.Background(), potent) {
		t.Fatal("control case: input must trigger auto-plan in normal mode")
	}

	c.SetCollaborationMode(CollabAsk)
	if c.shouldAutoPlan(context.Background(), potent) {
		t.Fatal("ask mode must suppress auto-plan: an explicit answer-only intent beats the heuristic")
	}
	if got := c.CollaborationMode(); got != CollabAsk {
		t.Fatalf("CollaborationMode() = %q, want %q", got, CollabAsk)
	}
}

func TestGoalCommandLeavesAskMode(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Working the goal.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 8)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone || e.Kind == event.Notice {
				events <- e
			}
		}),
	})

	c.SetCollaborationMode(CollabAsk)
	c.Submit("/goal ship the fix")
	waitForTurnDone(t, events)

	if got := c.CollaborationMode(); got != CollabNormal {
		t.Fatalf("/goal from ask mode: CollaborationMode() = %q, want %q (a goal is execution work)", got, CollabNormal)
	}
	if got := ag.Ceiling(); got != agent.CeilingFull {
		t.Fatalf("goal ceiling = %v, want CeilingFull", got)
	}
}

func TestStripComposePrefixesAskMarker(t *testing.T) {
	composed := AskModeMarker + "\n\nwhat does boot.Build do?"
	if got := StripComposePrefixes(composed); got != "what does boot.Build do?" {
		t.Fatalf("StripComposePrefixes = %q, want the bare question", got)
	}
}
