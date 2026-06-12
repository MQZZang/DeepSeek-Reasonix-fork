package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(submitPlan{}) }

// submitPlan is the explicit plan-proposal signal for plan mode. The model
// calls it with a structured plan (title + phases/steps); Execute only
// validates the shape and acks — the controller owns everything stateful:
// it detects the call in the turn's session messages, persists the plan
// artifact (.reasonix/plans/<id>.md), raises the approval gate, and seeds
// the task list from the same structured arguments on approval. Keeping the
// tool side-effect-free means it needs no write permission and stays callable
// under the read-only ceiling, and a plain text reply in plan mode goes back
// to being conversation instead of an implicit proposal.
//
// The tool is registered permanently (like every builtin) so the schema list
// never changes with the collaboration mode — the prompt-cache prefix stays
// byte-stable across mode toggles (G1).
type submitPlan struct{}

// submitPlanPhase mirrors the controller-side contract
// (internal/control/plan_artifact.go); the JSON schema below is the single
// source of truth both sides are tested against.
type submitPlanPhase struct {
	Name  string   `json:"name"`
	Steps []string `json:"steps,omitempty"`
}

func (submitPlan) Name() string { return "submit_plan" }

func (submitPlan) Description() string {
	return "Submit your finished plan for user approval (plan mode). Pass a short imperative `title` and 2-6 `phases`, each with a `name` (a coherent milestone) and its concrete, verifiable `steps`. The harness records the structured plan, raises the approval gate when your turn ends, and seeds the task list from it on approval — a plain text reply does NOT raise the gate. Outside plan mode the submission is only recorded as a structured note. After calling it, present the same plan to the user in your reply as a two-level markdown list, then stop."
}

func (submitPlan) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "title":{"type":"string","description":"Short imperative plan title, e.g. \"Add the config loader\"."},
  "phases":{
    "type":"array",
    "description":"The plan's phases in execution order (about 2-6). Each phase is a coherent milestone.",
    "items":{
      "type":"object",
      "properties":{
        "name":{"type":"string","description":"The phase as an imperative milestone, e.g. \"Wire the loader into boot\"."},
        "steps":{"type":"array","items":{"type":"string"},"description":"The phase's concrete, verifiable sub-steps, in order."}
      },
      "required":["name"]
    }
  }
},
"required":["title","phases"]
}`)
}

// ReadOnly is true: submit_plan only records a proposal (no filesystem or
// process effect — the controller does the artifact write), so it never needs
// approval and stays available under the read-only plan-mode ceiling, where
// proposing a plan is exactly the point.
func (submitPlan) ReadOnly() bool { return true }

func (submitPlan) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Title  string            `json:"title"`
		Phases []submitPlanPhase `json:"phases"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.Title) == "" {
		return "", fmt.Errorf("title is required")
	}
	if len(p.Phases) == 0 {
		return "", fmt.Errorf("at least one phase is required")
	}
	steps := 0
	for i, ph := range p.Phases {
		if strings.TrimSpace(ph.Name) == "" {
			return "", fmt.Errorf("phase %d: name is required", i+1)
		}
		steps += len(ph.Steps)
	}
	return fmt.Sprintf("Plan %q submitted (%d phases, %d steps). Present the same plan in your reply as a two-level markdown list, then stop — if plan mode is active the user will be asked to approve it when this turn ends.", strings.TrimSpace(p.Title), len(p.Phases), steps), nil
}
