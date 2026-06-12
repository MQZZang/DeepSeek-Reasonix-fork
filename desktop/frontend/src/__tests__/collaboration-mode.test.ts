// Run: tsx src/__tests__/collaboration-mode.test.ts

import { cycleCollaborationMode, parseAskCommand } from "../lib/collaborationMode";
import { normalizeCollaborationMode } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\ncollaboration mode");

eq(cycleCollaborationMode("normal"), "plan", "normal cycles to plan");
eq(cycleCollaborationMode("plan"), "ask", "plan cycles to ask");
eq(cycleCollaborationMode("ask"), "normal", "ask cycles to normal");
eq(cycleCollaborationMode("goal"), "goal", "goal is not in the shift+tab cycle");

eq(normalizeCollaborationMode("ask"), "ask", "normalize accepts ask");

eq(parseAskCommand("/ask")?.action, "on", "/ask enters ask mode");
eq(parseAskCommand("/ask on")?.action, "on", "/ask on enters ask mode");
eq(parseAskCommand("/ask off")?.action, "off", "/ask off leaves ask mode");
eq(parseAskCommand("/ask what is main.go?")?.text, "what is main.go?", "/ask <question> captures text");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
