import { type CollaborationMode } from "./types";

export type AskCommandAction = "on" | "off" | "question";

export interface ParsedAskCommand {
  action: AskCommandAction;
  text?: string;
}

/** Shift+Tab cycle: normal → plan → ask → normal. Goal is not in the cycle. */
export function cycleCollaborationMode(current: CollaborationMode): CollaborationMode {
  if (current === "goal") return current;
  switch (current) {
    case "plan":
      return "ask";
    case "ask":
      return "normal";
    default:
      return "plan";
  }
}

/** Mirrors control.ParseAskCommand for desktop slash handling. */
export function parseAskCommand(input: string): ParsedAskCommand | null {
  const trimmed = input.trim();
  if (trimmed !== "/ask" && !trimmed.startsWith("/ask ") && !trimmed.startsWith("/ask\t")) {
    return null;
  }
  const args = trimmed.slice("/ask".length).trim();
  const lower = args.toLowerCase();
  if (!args || lower === "on") return { action: "on" };
  if (lower === "off" || lower === "exit" || lower === "stop" || lower === "clear") {
    return { action: "off" };
  }
  return { action: "question", text: args };
}
