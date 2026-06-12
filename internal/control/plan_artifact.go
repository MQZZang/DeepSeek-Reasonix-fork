package control

// Plan artifacts (PR4): the controller-side half of the submit_plan contract.
//
// The submit_plan tool (internal/tool/builtin/submitplan.go) only validates
// and acks — everything stateful lives here. After a plan-mode turn ends, the
// controller scans the turn's session messages for the last well-formed
// submit_plan call, persists it as a markdown artifact with a status
// frontmatter (.reasonix/plans/<id>.md, status: draft → approved|rejected →
// executed), raises the approval gate, and on approval seeds the task list
// from the same structured arguments — replacing the old "any non-empty text
// reply is a proposal" heuristic (Gate-2 case A3) and the markdown-parse todo
// seeding (case B1).
//
// The artifact is informative, not load-bearing: gate flow and todo seeding
// work identically when no workspace root is configured (headless/tests) or
// when the write fails — those paths degrade to a log line, never an error.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/provider"
)

// planSubmitTool is the registry name of the builtin plan-submission tool.
const planSubmitTool = "submit_plan"

// PlanPhase is one milestone of a submitted plan, with its ordered steps.
type PlanPhase struct {
	Name  string   `json:"name"`
	Steps []string `json:"steps,omitempty"`
}

// PlanSubmission is the parsed argument payload of a submit_plan call. It is
// the controller-side mirror of the tool's JSON schema; parsePlanSubmission
// applies the same validation the tool's Execute applies, so a submission the
// tool acked always parses here and vice versa.
type PlanSubmission struct {
	Title  string      `json:"title"`
	Phases []PlanPhase `json:"phases"`
}

// parsePlanSubmission strictly parses submit_plan args. ok is false for
// malformed JSON, a blank title, zero phases, or a blank phase name —
// mirroring the tool's own validation so both sides agree on what counts as
// a submission.
func parsePlanSubmission(args string) (PlanSubmission, bool) {
	var s PlanSubmission
	if err := json.Unmarshal([]byte(args), &s); err != nil {
		return PlanSubmission{}, false
	}
	s.Title = strings.TrimSpace(s.Title)
	if s.Title == "" || len(s.Phases) == 0 {
		return PlanSubmission{}, false
	}
	for i := range s.Phases {
		s.Phases[i].Name = strings.TrimSpace(s.Phases[i].Name)
		if s.Phases[i].Name == "" {
			return PlanSubmission{}, false
		}
	}
	return s, true
}

// planSubmissionFromTurn scans this turn's session messages (History()[start:])
// for submit_plan calls and returns the LAST well-formed one — the model may
// revise its plan mid-turn, and the latest call is its final proposal. ok is
// false when the turn contains no parseable submission.
func (c *Controller) planSubmissionFromTurn(start int) (PlanSubmission, bool) {
	msgs := c.History()
	if start < 0 || start > len(msgs) {
		start = 0
	}
	var (
		found PlanSubmission
		ok    bool
	)
	for _, m := range msgs[start:] {
		if m.Role != provider.RoleAssistant {
			continue
		}
		for _, call := range m.ToolCalls {
			if call.Name != planSubmitTool {
				continue
			}
			if s, valid := parsePlanSubmission(call.Arguments); valid {
				found, ok = s, true
			}
		}
	}
	return found, ok
}

// renderPlanMarkdown renders a submission as the canonical two-level markdown
// list the plan-mode marker asks the model to present: numbered phases with
// indented step bullets. The same shape parsePlanTodos understands, so the
// artifact body round-trips through the legacy parser if ever needed.
func renderPlanMarkdown(s PlanSubmission) string {
	var b strings.Builder
	for i, ph := range s.Phases {
		fmt.Fprintf(&b, "%d. %s\n", i+1, ph.Name)
		for _, step := range ph.Steps {
			step = strings.TrimSpace(step)
			if step == "" {
				continue
			}
			fmt.Fprintf(&b, "   - %s\n", step)
		}
	}
	return b.String()
}

// planTodosArgsFromSubmission builds todo_write-shaped args JSON straight from
// the structured submission: each phase a level-0 item, each step a level-1
// item beneath it, first item in_progress, capped like the markdown parser.
// This replaces markdown parsing as the primary seeding path (B1): the seed
// can no longer drift from the approved plan because both come from the same
// arguments.
func planTodosArgsFromSubmission(s PlanSubmission) string {
	var todos []seedTodo
	add := func(content string, level int) bool {
		status := "pending"
		if len(todos) == 0 {
			status = "in_progress"
		}
		todos = append(todos, seedTodo{Content: content, Status: status, Level: level})
		return len(todos) < 20
	}
	for _, ph := range s.Phases {
		if !add(ph.Name, 0) {
			break
		}
		more := true
		for _, step := range ph.Steps {
			step = strings.TrimSpace(step)
			if step == "" {
				continue
			}
			if more = add(step, 1); !more {
				break
			}
		}
		if !more {
			break
		}
	}
	if len(todos) == 0 {
		return ""
	}
	b, err := json.Marshal(map[string]any{"todos": todos})
	if err != nil {
		return ""
	}
	return string(b)
}

// Plan artifact statuses. A fresh artifact is draft; the approval gate moves
// it to approved or rejected; the end of the approved execution turn moves
// approved to executed. Files are append-only history — a revised submission
// after a rejection writes a NEW artifact rather than mutating the old one.
const (
	planStatusDraft    = "draft"
	planStatusApproved = "approved"
	planStatusRejected = "rejected"
	planStatusExecuted = "executed"
)

// planApprovedCheckpointPrefix labels the checkpoint opened at the moment a
// plan is approved, right before the execution sub-turn starts touching files.
// The rewind picker shows "plan-approved: <title>", making "back to just
// before the agent started doing the work" a single visible jump.
const planApprovedCheckpointPrefix = "plan-approved: "

// planArtifactID builds the artifact's filename stem: a sortable UTC stamp
// plus a slug of the title.
func planArtifactID(now time.Time, title string) string {
	return now.UTC().Format("20060102-150405") + "-" + planSlug(title)
}

// planSlug reduces a title to a filesystem-safe slug (lowercase ASCII
// alphanumerics joined by single dashes, capped at 40 bytes, "plan" when
// nothing survives — e.g. a fully non-ASCII title).
func planSlug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
		if b.Len() >= 40 {
			break
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "plan"
	}
	return s
}

// writePlanArtifact persists a draft artifact under
// <workspaceRoot>/.reasonix/plans/ and returns its path, or "" when no
// workspace root is configured or the write fails (logged, never fatal —
// the gate and seeding do not depend on the file).
func (c *Controller) writePlanArtifact(s PlanSubmission) string {
	root := c.WorkspaceRoot()
	if root == "" {
		return ""
	}
	id := planArtifactID(time.Now(), s.Title)
	dir := filepath.Join(root, ".reasonix", "plans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("plan artifact: mkdir", "dir", dir, "err", err)
		return ""
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\n", id)
	fmt.Fprintf(&b, "title: %s\n", s.Title)
	fmt.Fprintf(&b, "status: %s\n", planStatusDraft)
	fmt.Fprintf(&b, "created_at: %s\n", time.Now().UTC().Format(time.RFC3339))
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", s.Title)
	b.WriteString(renderPlanMarkdown(s))
	path := filepath.Join(dir, id+".md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		slog.Warn("plan artifact: write", "path", path, "err", err)
		return ""
	}
	return path
}

// setPlanArtifactStatus rewrites the artifact's frontmatter status line in
// place. A "" path (artifact was never written) or a rewrite failure is a
// no-op beyond a log line — status tracking is best-effort by design.
func setPlanArtifactStatus(path, status string) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("plan artifact: read for status update", "path", path, "err", err)
		return
	}
	lines := strings.SplitN(string(data), "\n", -1)
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(line, "status: ") {
			lines[i] = "status: " + status
			replaced = true
			break
		}
		if i > 0 && line == "---" {
			break // end of frontmatter without a status line — leave as-is
		}
	}
	if !replaced {
		slog.Warn("plan artifact: no status line to update", "path", path)
		return
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		slog.Warn("plan artifact: write status", "path", path, "err", err)
	}
}
