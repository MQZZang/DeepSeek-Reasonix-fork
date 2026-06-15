package control

import (
	"context"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/skill"
)

// Collaboration modes — the intent axis of a session, orthogonal to the tool
// approval axis (ask/auto/yolo). Plan and Ask both run under the agent's
// read-only capability ceiling; they differ in lifecycle: plan gates its
// proposal behind an approval, ask just answers and stops.
const (
	CollabNormal = "normal"
	CollabPlan   = "plan"
	CollabAsk    = "ask"
)

// NormalizeCollaborationMode maps free-form input to a known collaboration
// mode, defaulting to normal.
func NormalizeCollaborationMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case CollabPlan:
		return CollabPlan
	case CollabAsk:
		return CollabAsk
	default:
		return CollabNormal
	}
}

// PlanModeMarker is prepended to every user turn while plan mode is on. It rides
// in the user message (not the system prompt or tools), so the cache-stable
// prompt prefix is left untouched and the toggle costs nothing in cache hits.
const PlanModeMarker = "[Plan mode — read-only. Explore the codebase first (read_file, ls, grep, glob, web_fetch, bash (read-only commands such as git log / git diff / go vet / grep), task, ask are available; writer tools and side-effecting bash are refused by the harness). Before planning, if a decision that is genuinely the user's — tech stack, an ambiguous requirement, scope, an irreversible choice — would materially shape the plan and you can't settle it from the codebase or a sensible default, use the ask tool to clarify it first; otherwise pick the obvious default and state the assumption in the plan instead of asking. When the plan is ready, submit it by calling the submit_plan tool: a short imperative title plus 2-6 phases (each a coherent milestone, e.g. \"Add the config loader\"), each phase carrying its concrete, verifiable steps (e.g. \"parse the TOML into Config\"). Only submit_plan raises the approval gate — a plain text reply is conversation, not a proposal, so answer questions freely. After calling submit_plan, present the same plan in your reply as a two-level markdown list (each PHASE a top-level numbered item, its steps indented bullets beneath; do NOT write phases as markdown headings) and stop — do not write files, edit, or run side-effecting bash. The user will be asked to approve before any changes are made.]"

// planSubmitNudgeMessage is the one-shot synthetic user turn injected when a
// plan-mode reply LOOKS like a plan (markdown list items) but the model never
// called submit_plan. The model is the only party who knows whether its text
// was a proposal, so it gets exactly one chance to submit; declining (another
// reply without a submission) ends the turn without a gate. Listed in
// syntheticPrefixes so UIs never render it as a user bubble.
const planSubmitNudgeMessage = "You wrote what looks like a plan but did not call submit_plan. If that was your proposed plan, call submit_plan now with its title and phases, then stop. If you were only answering or discussing, reply briefly without calling submit_plan."

// planReviseMessage is the synthetic user turn that carries the "request
// changes" feedback from the plan approval card back to the model. The user's
// feedback text is appended verbatim. Listed in syntheticPrefixes so UIs never
// render the wrapper as a user bubble — frontends echo the raw feedback their
// own way.
const planReviseMessage = "The user reviewed the submitted plan and requests changes before approving it. Plan mode is still on. Revise the plan accordingly and submit the updated plan with submit_plan, then present it briefly and stop. Requested changes: "

// AskModeMarker is prepended to every user turn while ask mode is on. Same
// turn-tail discipline as PlanModeMarker: the cache-stable prefix never moves.
const AskModeMarker = "[Ask mode — answer-only. Explore with read-only tools (read_file, ls, grep, glob, web_fetch, bash (read-only commands such as git log / git diff / go vet / grep), task); writer tools and side-effecting bash are refused by the harness. Answer the question directly and cite concrete evidence (file paths, line numbers, command output) where it helps. Do NOT change files or start making changes in this mode — if the request actually needs changes, say so in one short line and suggest switching to Agent mode (or Plan mode for larger work), then stop.]"

const (
	activeGoalOpen  = "<active-goal>"
	activeGoalClose = "</active-goal>"
)

const (
	GoalStatusRunning  = "running"
	GoalStatusComplete = "complete"
	GoalStatusBlocked  = "blocked"
	GoalStatusStopped  = "stopped"
	// GoalStatusPaused marks a goal that is suspended but resumable: the model
	// went markerless twice in a row, the user paused it (/goal pause), the
	// turn was cancelled, or the goal was restored from the session sidecar on
	// resume. The objective text is kept; /goal resume continues the loop.
	GoalStatusPaused = "paused"
)

// GoalContinuePrompt is the synthetic user turn that resumes a goal loop after
// /goal resume or an auto-continue iteration.
const GoalContinuePrompt = "Continue pursuing the active goal. If it is complete, provide the concise final result and end with [goal:complete]. If it is truly blocked on a user-owned decision after trying sensible defaults, end with [goal:blocked:<short reason>]. Otherwise do the next useful work and end with [goal:continue]."

// StripComposePrefixes removes controller-injected prefixes from a composed
// user message so that the display text matches what the user actually typed.
// It strips the PlanModeMarker plus transient XML blocks such as
// <reasoning-language>, <memory-update>, and <background-jobs> that Compose
// prepends to user turns. This is used as a fallback when no .display.json
// sidecar recording exists (e.g. sessions created before the display-recording
// feature, or synthetic user messages injected by the controller).
func StripComposePrefixes(content string) string {
	s := agent.StripTransientUserBlocks(content)
	s = strings.TrimPrefix(s, PlanModeMarker+"\n\n")
	s = strings.TrimPrefix(s, PlanModeMarker)
	s = strings.TrimPrefix(s, AskModeMarker+"\n\n")
	s = strings.TrimPrefix(s, AskModeMarker)
	s = strings.TrimSpace(s)
	return s
}

// IsSyntheticUserMessage returns true if the content matches one of the known
// synthetic user messages injected by the controller or agent loop (plan
// approval, stream recovery, readiness retry, etc.). These should not be shown
// in the chat UI.
func IsSyntheticUserMessage(content string) bool {
	trimmed := strings.TrimSpace(agent.StripTransientUserBlocks(content))
	if trimmed == planApprovedMessage {
		return true
	}
	for _, prefix := range syntheticPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// syntheticPrefixes must be kept in sync with the synthetic user messages
// injected by the controller (planApprovedMessage), agent loop
// (streamRecoveryMessage, finalReadinessRetryMessage, emptyFinalRetryMessage,
// executorHandoffRetryMessage in internal/agent/agent.go), and compaction
// folds (internal/agent/compact.go), which store summaries as user-role
// messages the chat UI must never render as user bubbles (#3653).
var syntheticPrefixes = []string{
	"Plan approved — plan mode is off",
	"You wrote what looks like a plan but did not call submit_plan",
	"The user reviewed the submitted plan and requests changes",
	"Host final-answer readiness check failed",
	"You are already in the executor phase",
	"The previous assistant response was interrupted while a tool call",
	"The previous assistant response was interrupted during streaming",
	"The previous assistant response was interrupted before visible",
	"The previous assistant response finished without any visible answer",
	"<compaction-summary>",
	"Summary of the later conversation (compacted from here on):",
	"Summary of earlier conversation (compacted up to here):",
}

// Compose applies the collaboration-mode marker to a turn's text when plan or
// ask mode is on, returning the message to actually send to the model. The
// frontend keeps showing the raw text as the user bubble.
func (c *Controller) Compose(text string) string {
	c.mu.Lock()
	collab := c.collabMode
	goal := c.goal
	goalStatus := c.goalStatus
	reasoningLanguage := c.reasoningLanguage
	notes := c.pendingMemory
	c.pendingMemory = nil
	c.mu.Unlock()

	if strings.TrimSpace(goal) != "" && goalStatus == GoalStatusRunning {
		text = activeGoalBlock(goal) + "\n\n" + text
	}
	switch collab {
	case CollabPlan:
		text = PlanModeMarker + "\n\n" + text
	case CollabAsk:
		text = AskModeMarker + "\n\n" + text
	}
	text = agent.WithReasoningLanguage(text, reasoningLanguage)

	// Memory added mid-session rides the turn (never the cached system prefix),
	// so it takes effect now without invalidating the prompt cache. It folds into
	// the system prefix on the next session, where it costs nothing per turn.
	if len(notes) > 0 {
		var b strings.Builder
		b.WriteString("<memory-update>\n")
		b.WriteString("The following project-memory changes were just made and apply from now on:\n")
		for _, n := range notes {
			b.WriteString("- " + n + "\n")
		}
		b.WriteString("</memory-update>\n\n")
		text = b.String() + text
	}

	// Background jobs that finished since the last turn ride the turn too, so the
	// model learns of completions even though the user-facing notices don't reach
	// its context. Like memory, this never touches the cache-stable prefix.
	if c.jobs != nil {
		if note := c.jobs.DrainCompletedNoteForSession(c.parentSessionID()); note != "" {
			text = "<background-jobs>\n" + note + "\n</background-jobs>\n\n" + text
		}
	}
	return text
}

func reasoningLanguageBlock(lang string) string {
	return agent.ReasoningLanguageBlock(lang)
}

func (c *Controller) ComposeSynthetic(text string) string {
	c.mu.Lock()
	lang := c.reasoningLanguage
	c.mu.Unlock()
	return agent.WithReasoningLanguage(text, lang)
}

func activeGoalBlock(goal string) string {
	goal = strings.TrimSpace(goal)
	goal = strings.ReplaceAll(goal, activeGoalClose, "<\\/active-goal>")
	var b strings.Builder
	b.WriteString(activeGoalOpen)
	b.WriteString("\n")
	b.WriteString(goal)
	b.WriteString("\n\n")
	b.WriteString("Goal mode: pursue this goal autonomously. Keep working across turns until the goal is complete. Prefer sensible defaults over asking the user; use ask only when you are truly blocked on a user-owned decision. Do not stop after describing a plan; execute the next useful step. End every goal-mode assistant reply with exactly one status marker on its own line: [goal:continue], [goal:complete], or [goal:blocked:<short reason>].")
	b.WriteString("\n")
	b.WriteString(activeGoalClose)
	return b.String()
}

// MemoryQuickAddNote parses the "# <note>" memory shortcut. The space after
// "#" is intentional: "#7", "#issue", and "#标题" are ordinary user prompts,
// not memory writes. Multi-line input starting with "# " is NOT treated as a
// quick-add note — it is almost certainly a Markdown heading in a structured
// prompt (e.g. "# Context\n\n- file.go\n# Objective"). Only single-line input
// may be a quick-add note.
func MemoryQuickAddNote(input string) (note string, ok bool) {
	trimmed := strings.TrimSpace(input)
	if strings.Contains(trimmed, "\n") {
		return "", false
	}
	if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "#\t") {
		return strings.TrimSpace(trimmed[1:]), true
	}
	return "", false
}

// RememberCommandNote parses the explicit "/remember <note>" memory command.
func RememberCommandNote(input string) (note string, ok bool) {
	trimmed := strings.TrimSpace(input)
	switch {
	case trimmed == "/remember":
		return "", true
	case strings.HasPrefix(trimmed, "/remember ") || strings.HasPrefix(trimmed, "/remember\t"):
		return strings.TrimSpace(trimmed[len("/remember"):]), true
	default:
		return "", false
	}
}

type GoalCommandAction int

const (
	GoalCommandStatus GoalCommandAction = iota + 1
	GoalCommandSet
	GoalCommandClear
	GoalCommandPause
	GoalCommandResume
)

type GoalCommand struct {
	Action GoalCommandAction
	Text   string
}

// ParseGoalCommand parses the "/goal" family. Bare keywords act on the active
// goal (status / pause / resume / clear); anything else is a new objective.
// Only the bare word is a verb — "/goal resume the migration" sets a goal.
func ParseGoalCommand(input string) (GoalCommand, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed != "/goal" && !strings.HasPrefix(trimmed, "/goal ") && !strings.HasPrefix(trimmed, "/goal\t") {
		return GoalCommand{}, false
	}
	args := strings.TrimSpace(trimmed[len("/goal"):])
	switch strings.ToLower(args) {
	case "", "status":
		return GoalCommand{Action: GoalCommandStatus}, true
	case "clear", "off", "stop", "done":
		return GoalCommand{Action: GoalCommandClear}, true
	case "pause":
		return GoalCommand{Action: GoalCommandPause}, true
	case "resume", "continue":
		return GoalCommand{Action: GoalCommandResume}, true
	default:
		return GoalCommand{Action: GoalCommandSet, Text: args}, true
	}
}

// AskCommand is a parsed "/ask …" line. Bare "/ask" turns ask mode on,
// "/ask off" leaves it, and "/ask <question>" turns it on and asks right away.
type AskCommandAction int

const (
	AskCommandOn AskCommandAction = iota
	AskCommandOff
	AskCommandQuestion
)

type AskCommand struct {
	Action AskCommandAction
	Text   string
}

func ParseAskCommand(input string) (AskCommand, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed != "/ask" && !strings.HasPrefix(trimmed, "/ask ") && !strings.HasPrefix(trimmed, "/ask\t") {
		return AskCommand{}, false
	}
	args := strings.TrimSpace(trimmed[len("/ask"):])
	switch strings.ToLower(args) {
	case "", "on":
		return AskCommand{Action: AskCommandOn}, true
	case "off", "exit", "stop", "clear":
		return AskCommand{Action: AskCommandOff}, true
	default:
		return AskCommand{Action: AskCommandQuestion, Text: args}, true
	}
}

// CustomCommand resolves a "/name args…" line against the loaded custom slash
// commands, returning the rendered prompt to send (found=false when no command
// matches). It does not apply the plan-mode marker — call Compose for that.
func (c *Controller) CustomCommand(input string) (sent string, found bool) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", false
	}
	name := strings.TrimPrefix(fields[0], "/")
	for _, cmd := range c.commands {
		if cmd.Name == name {
			return cmd.Render(fields[1:]), true
		}
	}
	return "", false
}

// RunSkill resolves a "/<name> args…" line against the loaded skills, returning
// the skill's rendered body to send as a turn (found=false when no skill
// matches). Invoking a skill by slash always inlines its body — the model reads
// and follows the playbook in the main loop; a subagent skill's isolation is
// only engaged when the model calls it via run_skill / the dedicated tool. The
// caller applies Compose for plan-mode/memory framing.
func (c *Controller) RunSkill(input string) (sent string, found bool) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", false
	}
	name := strings.TrimPrefix(fields[0], "/")
	if sk, ok := c.skillByName(name); ok {
		return skill.Render(sk, strings.Join(fields[1:], " ")), true
	}
	return "", false
}

func (c *Controller) skillByName(name string) (skill.Skill, bool) {
	if c.skillStore != nil {
		return c.skillStore.Read(name)
	}
	for _, sk := range c.skills {
		if sk.Name == name {
			return sk, true
		}
	}
	return skill.Skill{}, false
}

// MCPPrompt resolves a "/mcp__server__prompt args…" line: it maps the positional
// args onto the prompt's declared arguments and fetches the rendered prompt from
// the MCP server (an async prompts/get). found is false when no such prompt
// exists; err carries a fetch failure. Honours ctx.
func (c *Controller) MCPPrompt(ctx context.Context, input string) (sent string, found bool, err error) {
	if c.host == nil {
		return "", false, nil
	}
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", false, nil
	}
	name := strings.TrimPrefix(fields[0], "/")

	prompts := c.host.Prompts()
	idx := -1
	for i := range prompts {
		if prompts[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", false, nil
	}

	args := map[string]string{}
	for i, a := range prompts[idx].Args {
		if i+1 < len(fields) {
			args[a.Name] = fields[i+1]
		}
	}
	text, err := prompts[idx].Get(ctx, args)
	if err != nil {
		return "", true, err
	}
	return text, true, nil
}
