package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestCeilingZeroValueIsFull pins the default: a fresh agent is unrestricted,
// so existing constructors keep today's behaviour without touching the ceiling.
func TestCeilingZeroValueIsFull(t *testing.T) {
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	if got := a.Ceiling(); got != CeilingFull {
		t.Fatalf("fresh agent ceiling = %v, want CeilingFull", got)
	}
}

// TestSetPlanModeDelegatesToCeiling pins the compatibility shim contract:
// plan-mode callers keep working, expressed through the ceiling primitive.
func TestSetPlanModeDelegatesToCeiling(t *testing.T) {
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)

	a.SetPlanMode(true)
	if got := a.Ceiling(); got != CeilingReadOnly {
		t.Fatalf("after SetPlanMode(true) ceiling = %v, want CeilingReadOnly", got)
	}
	a.SetPlanMode(false)
	if got := a.Ceiling(); got != CeilingFull {
		t.Fatalf("after SetPlanMode(false) ceiling = %v, want CeilingFull", got)
	}
}

// TestSetCeilingReadOnlyBlocksWritersDirectly drives the executeOne gate via
// the primitive itself, not the shim: the ceiling must stand on its own for
// the modes that will consume it without any plan-mode involvement.
func TestSetCeilingReadOnlyBlocksWritersDirectly(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_only_tool", readOnly: true})
	reg.Add(fakeTool{name: "writer_tool", readOnly: false})

	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	a.SetCeiling(CeilingReadOnly)

	ro := a.executeOne(context.Background(), provider.ToolCall{Name: "read_only_tool"})
	if ro.blocked {
		t.Fatalf("read-only tool blocked under CeilingReadOnly: %q", ro.output)
	}
	wr := a.executeOne(context.Background(), provider.ToolCall{Name: "writer_tool"})
	if !wr.blocked || !strings.HasPrefix(wr.output, "blocked:") {
		t.Fatalf("writer under CeilingReadOnly = (blocked=%v, %q), want a blocked: result", wr.blocked, wr.output)
	}

	a.SetCeiling(CeilingFull)
	wr2 := a.executeOne(context.Background(), provider.ToolCall{Name: "writer_tool"})
	if wr2.blocked {
		t.Fatalf("writer blocked after restoring CeilingFull: %q", wr2.output)
	}
}

// classifierTool is a statically-writer tool implementing the commandReadOnly
// optional interface: IsReadOnlyCommand reports per-invocation read-only
// status from a scripted classifier, mirroring how bash and task implement it.
type classifierTool struct {
	fakeTool
	classify func(json.RawMessage) bool
}

func (c classifierTool) IsReadOnlyCommand(args json.RawMessage) bool { return c.classify(args) }

// TestCeilingAdmitsCommandReadOnlyInvocation pins the PR3 refinement: under
// CeilingReadOnly a statically-writer tool whose IsReadOnlyCommand(args)
// returns true is admitted past the ceiling, while a false classification
// keeps the canonical blocked result. The static ReadOnly() flag is untouched,
// so batch partitioning stays conservative.
func TestCeilingAdmitsCommandReadOnlyInvocation(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(classifierTool{
		fakeTool: fakeTool{name: "bash", readOnly: false},
		classify: func(args json.RawMessage) bool {
			var p struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal(args, &p)
			return strings.HasPrefix(p.Command, "git log")
		},
	})

	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	a.SetCeiling(CeilingReadOnly)

	ro := a.executeOne(context.Background(), provider.ToolCall{Name: "bash", Arguments: `{"command":"git log -3"}`})
	if ro.blocked {
		t.Fatalf("read-only-classified bash blocked under CeilingReadOnly: %q", ro.output)
	}
	wr := a.executeOne(context.Background(), provider.ToolCall{Name: "bash", Arguments: `{"command":"rm -rf out"}`})
	if !wr.blocked || wr.errMsg != "blocked: read-only mode" {
		t.Fatalf("writer-classified bash = (blocked=%v, errMsg=%q), want blocked with canonical message", wr.blocked, wr.errMsg)
	}
}

// TestOptionsCeilingAppliesAtConstruction pins the Options.Ceiling seam the
// task tool uses for ceiling inheritance: a sub-agent built with
// Options{Ceiling: CeilingReadOnly} starts restricted without any SetCeiling
// call, and the zero value stays CeilingFull.
func TestOptionsCeilingAppliesAtConstruction(t *testing.T) {
	restricted := New(nil, tool.NewRegistry(), NewSession(""), Options{Ceiling: CeilingReadOnly}, event.Discard)
	if got := restricted.Ceiling(); got != CeilingReadOnly {
		t.Fatalf("Options{Ceiling: CeilingReadOnly} agent ceiling = %v, want CeilingReadOnly", got)
	}
	unrestricted := New(nil, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	if got := unrestricted.Ceiling(); got != CeilingFull {
		t.Fatalf("zero-value Options agent ceiling = %v, want CeilingFull", got)
	}
}

// ceilingProbeTool records the ceiling stamped on its Execute context, proving
// executeOne propagates the parent ceiling to tools (the seam task uses to
// inherit the ceiling into its sub-agent).
type ceilingProbeTool struct {
	fakeTool
	seen *Ceiling
}

func (c ceilingProbeTool) IsReadOnlyCommand(json.RawMessage) bool { return true }
func (c ceilingProbeTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	*c.seen = CeilingFromContext(ctx)
	return "ok", nil
}

// TestExecuteOneStampsCeilingOntoContext pins the context plumbing end of the
// inheritance contract: a tool admitted under CeilingReadOnly observes that
// ceiling via CeilingFromContext, and under CeilingFull observes CeilingFull.
func TestExecuteOneStampsCeilingOntoContext(t *testing.T) {
	var seen Ceiling
	reg := tool.NewRegistry()
	reg.Add(ceilingProbeTool{
		fakeTool: fakeTool{name: "task", readOnly: false},
		seen:     &seen,
	})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)

	a.SetCeiling(CeilingReadOnly)
	if out := a.executeOne(context.Background(), provider.ToolCall{Name: "task", Arguments: `{}`}); out.blocked {
		t.Fatalf("ceiling-inheriting task blocked under CeilingReadOnly: %q", out.output)
	}
	if seen != CeilingReadOnly {
		t.Fatalf("tool saw ceiling %v under read-only parent, want CeilingReadOnly", seen)
	}

	a.SetCeiling(CeilingFull)
	if out := a.executeOne(context.Background(), provider.ToolCall{Name: "task", Arguments: `{}`}); out.blocked {
		t.Fatalf("task blocked under CeilingFull: %q", out.output)
	}
	if seen != CeilingFull {
		t.Fatalf("tool saw ceiling %v under full parent, want CeilingFull", seen)
	}
}
