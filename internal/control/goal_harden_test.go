package control

// PR7 tests: goal hardening. The paused state (markerless×2, user pause,
// cancellation), /goal pause|resume commands, and the branch-meta sidecar
// persistence that lets an in-flight goal survive restart/resume. Companions:
// goal_test.go (F1/F3 loop contracts), baseline_goal_test.go (F2/F4).

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

// TestGoalMarkerlessCounterResetByMarker: a single markerless slip between
// properly marked replies never pauses the goal — only two in a row do.
func TestGoalMarkerlessCounterResetByMarker(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Slip one, no marker."),
		textTurn("Marked progress.\n\n[goal:continue]"),
		textTurn("Slip two, but not consecutive."),
		textTurn("Done.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 16)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				events <- e
			}
		}),
	})

	c.Submit("/goal implement the TODOs")
	waitForTurnDone(t, events)

	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want 4 (non-consecutive slips keep continuing)", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want complete", got)
	}
}

// TestParseGoalCommandPauseResume: bare keywords are verbs; anything longer
// sets a new objective.
func TestParseGoalCommandPauseResume(t *testing.T) {
	cases := []struct {
		in     string
		action GoalCommandAction
		text   string
	}{
		{"/goal pause", GoalCommandPause, ""},
		{"/goal Pause", GoalCommandPause, ""},
		{"/goal resume", GoalCommandResume, ""},
		{"/goal continue", GoalCommandResume, ""},
		{"/goal resume the migration", GoalCommandSet, "resume the migration"},
		{"/goal pause all deployments today", GoalCommandSet, "pause all deployments today"},
	}
	for _, tc := range cases {
		cmd, ok := ParseGoalCommand(tc.in)
		if !ok {
			t.Fatalf("ParseGoalCommand(%q) not recognised", tc.in)
		}
		if cmd.Action != tc.action || cmd.Text != tc.text {
			t.Fatalf("ParseGoalCommand(%q) = {%v %q}, want {%v %q}", tc.in, cmd.Action, cmd.Text, tc.action, tc.text)
		}
	}
}

// TestGoalPauseResumeMethods pins the state transitions: pause needs a
// running goal; resume accepts paused or blocked and clears the audits.
func TestGoalPauseResumeMethods(t *testing.T) {
	c := New(Options{})

	if c.PauseGoal() {
		t.Fatal("pausing with no goal must report false")
	}
	if c.ResumeGoal() {
		t.Fatal("resuming with no goal must report false")
	}

	c.SetGoal("ship it")
	if !c.PauseGoal() {
		t.Fatal("pausing a running goal must report true")
	}
	if got := c.GoalStatus(); got != GoalStatusPaused {
		t.Fatalf("GoalStatus() = %q, want paused", got)
	}
	if c.PauseGoal() {
		t.Fatal("pausing an already-paused goal must report false")
	}
	if got := c.Goal(); got != "ship it" {
		t.Fatalf("Goal() = %q, want the objective kept while paused", got)
	}

	if !c.ResumeGoal() {
		t.Fatal("resuming a paused goal must report true")
	}
	if got := c.GoalStatus(); got != GoalStatusRunning {
		t.Fatalf("GoalStatus() = %q, want running", got)
	}
}

// TestGoalCancelPausesGoal: cancellation at a goal-loop boundary suspends the
// goal (objective kept, resumable) rather than killing it.
func TestGoalCancelPausesGoal(t *testing.T) {
	ag := agent.New(nil, nil, agent.NewSession(""), agent.Options{}, event.Discard)
	c := New(Options{Executor: ag})
	c.SetGoal("ship the redesign")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.continueGoal(ctx); err == nil {
		t.Fatal("continueGoal on a cancelled context must return its error")
	}

	if got := c.GoalStatus(); got != GoalStatusPaused {
		t.Fatalf("GoalStatus() after cancel = %q, want paused", got)
	}
	if got := c.Goal(); got != "ship the redesign" {
		t.Fatalf("Goal() after cancel = %q, want the objective kept", got)
	}
}

// TestGoalPersistRoundTrip: every state change mirrors into the branch-meta
// sidecar — set stores a running record, pause updates it, clear removes it.
func TestGoalPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := agent.NewSessionPath(dir, "goalmeta")
	c := New(Options{SessionDir: dir})
	c.SetSessionPath(path)

	c.SetGoal("ship the redesign")
	m, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok || m.Goal == nil {
		t.Fatalf("after SetGoal: meta=%+v ok=%v err=%v, want a stored goal", m, ok, err)
	}
	if m.Goal.Text != "ship the redesign" || m.Goal.Status != GoalStatusRunning {
		t.Fatalf("stored goal = %+v, want running 'ship the redesign'", m.Goal)
	}

	c.PauseGoal()
	if m, _, _ = agent.LoadBranchMeta(path); m.Goal == nil || m.Goal.Status != GoalStatusPaused {
		t.Fatalf("after PauseGoal: stored goal = %+v, want paused", m.Goal)
	}

	c.ClearGoal()
	if m, _, _ = agent.LoadBranchMeta(path); m.Goal != nil {
		t.Fatalf("after ClearGoal: stored goal = %+v, want removed", m.Goal)
	}
}

// TestGoalResumeToSessionWithoutGoalClearsCarryOver: attaching to a session
// whose sidecar has no goal clears whatever goal the controller carried from
// the previously attached session.
func TestGoalResumeToSessionWithoutGoalClearsCarryOver(t *testing.T) {
	dir := t.TempDir()
	other := agent.NewSessionPath(dir, "plain")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := sess.Save(other); err != nil {
		t.Fatal(err)
	}

	ag := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionDir: dir})
	c.SetGoal("carried over")

	loaded, err := agent.LoadSession(other)
	if err != nil {
		t.Fatal(err)
	}
	c.Resume(loaded, other)

	if got := c.Goal(); got != "" {
		t.Fatalf("Goal() after resuming a goal-less session = %q, want cleared", got)
	}
}

// TestGoalRestoredBlockedStaysBlocked: a goal that was blocked when the
// session went away comes back blocked, reason intact — not paused.
func TestGoalRestoredBlockedStaysBlocked(t *testing.T) {
	dir := t.TempDir()
	path := agent.NewSessionPath(dir, "blocked")
	if _, err := agent.EnsureBranchMeta(path); err != nil {
		t.Fatal(err)
	}
	m, _, _ := agent.LoadBranchMeta(path)
	m.Goal = &agent.GoalState{Text: "deploy the service", Status: GoalStatusBlocked, Blocks: 3, Block: "needs credentials"}
	if err := agent.SaveBranchMeta(path, m); err != nil {
		t.Fatal(err)
	}

	ag := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionDir: dir})
	c.Resume(agent.NewSession("sys"), path)

	if got := c.Goal(); got != "deploy the service" {
		t.Fatalf("restored Goal() = %q", got)
	}
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("restored GoalStatus() = %q, want blocked", got)
	}
}

// TestGoalResumeCommandRestartsLoop: /goal resume on a paused goal restarts
// the continuation loop and announces the resumption.
func TestGoalResumeCommandRestartsLoop(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Finishing up.\n\n[goal:complete]"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	events := make(chan event.Event, 16)
	var notices []string
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.TurnDone:
				events <- e
			case event.Notice:
				notices = append(notices, e.Text)
			}
		}),
	})
	c.SetGoal("ship the redesign")
	c.PauseGoal()

	c.Submit("/goal resume")
	waitForTurnDone(t, events)

	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1 (the resumed continuation turn)", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("GoalStatus() = %q, want complete", got)
	}
	if !containsSubstring(notices, strings.SplitN(i18n.M.GoalResumedFmt, "%", 2)[0]) {
		t.Fatalf("notices = %q, want the goal-resumed notice", notices)
	}
}

// TestGoalPauseCommand: /goal pause suspends a running goal from idle; the
// status command then reports the paused state.
func TestGoalPauseCommand(t *testing.T) {
	var notices []string
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})})
	c.SetGoal("ship the redesign")

	c.Submit("/goal pause")
	if got := c.GoalStatus(); got != GoalStatusPaused {
		t.Fatalf("GoalStatus() = %q, want paused", got)
	}
	if !containsSubstring(notices, i18n.M.GoalPaused) {
		t.Fatalf("notices = %q, want the goal-paused notice", notices)
	}

	c.Submit("/goal")
	if !containsSubstring(notices, GoalStatusPaused) {
		t.Fatalf("notices = %q, want the status line to mention the paused state", notices)
	}
}

// TestGoalClearedOnNewSession: a goal is session-scoped and must not leak
// into (or pollute the sidecar of) a freshly rotated session.
func TestGoalClearedOnNewSession(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "kick off"})
	ag := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionDir: dir})
	c.SetSessionPath(agent.NewSessionPath(dir, "first"))
	c.SetGoal("ship the redesign")

	if err := c.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if got := c.Goal(); got != "" {
		t.Fatalf("Goal() after /new = %q, want cleared", got)
	}
	if m, ok, _ := agent.LoadBranchMeta(c.SessionPath()); ok && m.Goal != nil {
		t.Fatalf("fresh session meta carries a goal: %+v", m.Goal)
	}
}
