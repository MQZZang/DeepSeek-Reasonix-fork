package control

// Gate-2 baseline cases F2/F4: the Goal scenario's two former contract gaps,
// both RESOLVED(risk-7, PR7). F1 (auto-continue until complete) and F3
// (blocked×3 escalation) are pinned by goal_test.go; the pause/resume and
// persistence mechanics PR7 added are covered in goal_harden_test.go. See
// baseline_helpers_test.go for suite conventions.

import (
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestBaselineF2GoalMarkerlessRepliesPauseAfterTwo — Gate-2 case F2, resolved
// side (PR7, permanent contract).
//
// A goal-mode reply that omits the [goal:*] status marker is tolerated once
// (models do forget), but two in a row pause the goal instead of silently
// burning turns: the loop stops after the second markerless reply, the
// objective text is kept, and /goal resume continues where it left off with a
// fresh markerless audit.
func TestBaselineF2GoalMarkerlessRepliesPauseAfterTwo(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("Did some work, forgot the status marker."),
		textTurn("More work, still no marker."),
		// Served only after /goal resume:
		textTurn("Back on track, one more slip without a marker."),
		textTurn("Wrapped up.\n\n[goal:complete]"),
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

	c.Submit("/goal implement the three TODOs")
	waitForTurnDone(t, events)

	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2 (second markerless reply pauses the goal)", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusPaused {
		t.Fatalf("GoalStatus() = %q, want paused", got)
	}
	if got := c.Goal(); got != "implement the three TODOs" {
		t.Fatalf("Goal() = %q, want the objective kept while paused", got)
	}

	// Resume continues the loop with a clean markerless audit: one more slip
	// is tolerated again, then the complete marker ends the goal.
	c.Submit("/goal resume")
	waitForTurnDone(t, events)

	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want 4 (resume + one tolerated slip + complete)", prov.call)
	}
	if got := c.GoalStatus(); got != GoalStatusComplete {
		t.Fatalf("GoalStatus() after resume = %q, want complete", got)
	}
}

// TestBaselineF4GoalPersistsAcrossResume — Gate-2 case F4, resolved side
// (PR7, permanent contract).
//
// The goal state is mirrored into the session's branch-meta sidecar, so a
// restart/resume brings the objective back. A goal that was running when the
// session went away comes back paused — nothing autonomously resumes work on
// behalf of the user — and /goal resume continues it.
func TestBaselineF4GoalPersistsAcrossResume(t *testing.T) {
	dir := t.TempDir()
	path := agent.NewSessionPath(dir, "baseline")

	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "kick off"})
	exec1 := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c1 := New(Options{Executor: exec1, SessionDir: dir})
	c1.SetSessionPath(path)
	c1.SetGoal("ship the redesign")
	if c1.Goal() == "" || c1.GoalStatus() != GoalStatusRunning {
		t.Fatalf("precondition: goal=%q status=%q, want a running goal", c1.Goal(), c1.GoalStatus())
	}
	if err := c1.Snapshot(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	exec2 := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c2 := New(Options{Executor: exec2, SessionDir: dir})
	c2.Resume(loaded, path)

	if got := c2.Goal(); got != "ship the redesign" {
		t.Fatalf("resumed Goal() = %q, want the persisted objective", got)
	}
	if got := c2.GoalStatus(); got != GoalStatusPaused {
		t.Fatalf("resumed GoalStatus() = %q, want paused (restored goals never auto-run)", got)
	}
}
