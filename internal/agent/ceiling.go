package agent

// Ceiling is the agent's capability ceiling: the harness-enforced upper bound
// on what tool calls may do, independent of the approval axis (ask/auto/yolo)
// and of any collaboration mode the frontend presents. The ceiling is checked
// in executeOne before the permission gate, so no approval posture can pierce
// it. The system prompt, tool schemas, and message history never change with
// the ceiling — mode instructions ride the turn tail — so toggling it costs
// nothing in prompt-cache hits.
//
// Today plan mode is the only ReadOnly consumer (via the SetPlanMode shim);
// further modes reuse the same primitive instead of adding parallel booleans.
type Ceiling int32

const (
	// CeilingFull imposes no harness-side restriction; tool calls proceed to
	// the permission gate as usual. Zero value: a fresh Agent is unrestricted.
	CeilingFull Ceiling = 0
	// CeilingReadOnly refuses every tool whose ReadOnly() is false before the
	// permission gate, returning a "blocked" result the model can adapt to.
	CeilingReadOnly Ceiling = 1
)

func (c Ceiling) String() string {
	switch c {
	case CeilingReadOnly:
		return "read-only"
	default:
		return "full"
	}
}

// SetCeiling sets the capability ceiling for subsequent tool executions.
func (a *Agent) SetCeiling(c Ceiling) { a.ceiling.Store(int32(c)) }

// Ceiling reports the current capability ceiling.
func (a *Agent) Ceiling() Ceiling { return Ceiling(a.ceiling.Load()) }
