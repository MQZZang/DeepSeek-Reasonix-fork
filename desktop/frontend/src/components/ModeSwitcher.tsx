import { useCallback, useEffect, useRef, useState } from "react";
import { Bot, Check, ChevronsUpDown, List, MessageSquare, Target } from "lucide-react";
import { useT, type Translator } from "../lib/i18n";
import type { CollaborationMode, GoalStatus } from "../lib/types";
import { ANCHORED_POPOVER_CLOSE_MS, AnchoredPopover } from "./AnchoredPopover";

function modeLabel(
  mode: CollaborationMode,
  goalStatus: GoalStatus | undefined,
  hasGoal: boolean,
  t: Translator,
): string {
  if (mode === "goal" && hasGoal) {
    if (goalStatus === "paused") return t("composer.goalPaused");
    if (goalStatus === "blocked") return t("composer.goalBlocked");
    return t("composer.modeGoal");
  }
  switch (mode) {
    case "plan":
      return t("composer.modePlan");
    case "ask":
      return t("composer.collabAsk");
    case "goal":
      return t("composer.modeGoal");
    default:
      return t("composer.modeAgent");
  }
}

const MODE_OPTIONS: CollaborationMode[] = ["normal", "plan", "ask", "goal"];

function modeIcon(mode: CollaborationMode, size = 16) {
  switch (mode) {
    case "plan":
      return <List size={size} />;
    case "ask":
      return <MessageSquare size={size} />;
    case "goal":
      return <Target size={size} />;
    default:
      return <Bot size={size} />;
  }
}

export function ModeSwitcher({
  collaborationMode,
  goalStatus,
  goal,
  disabled,
  running,
  onPick,
}: {
  collaborationMode: CollaborationMode;
  goalStatus?: GoalStatus;
  goal?: string;
  disabled: boolean;
  running: boolean;
  onPick: (mode: CollaborationMode) => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [closing, setClosing] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const closeTimerRef = useRef<number | null>(null);
  const hasGoal = Boolean((goal ?? "").trim());
  const current = modeLabel(collaborationMode, goalStatus, hasGoal, t);
  const triggerMuted = collaborationMode === "normal" && !hasGoal;
  const triggerIcon = modeIcon(collaborationMode, 13);

  const clearCloseTimer = useCallback(() => {
    if (closeTimerRef.current === null) return;
    window.clearTimeout(closeTimerRef.current);
    closeTimerRef.current = null;
  }, []);

  const openMenu = useCallback(() => {
    clearCloseTimer();
    setClosing(false);
    setOpen(true);
  }, [clearCloseTimer]);

  const closeMenu = useCallback((afterClose?: () => void) => {
    clearCloseTimer();
    setClosing(true);
    window.requestAnimationFrame(() => setOpen(false));
    const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    closeTimerRef.current = window.setTimeout(() => {
      closeTimerRef.current = null;
      setClosing(false);
      afterClose?.();
    }, reduceMotion ? 0 : ANCHORED_POPOVER_CLOSE_MS);
  }, [clearCloseTimer]);

  useEffect(() => () => clearCloseTimer(), [clearCloseTimer]);

  const pick = (mode: CollaborationMode) => {
    closeMenu(() => {
      if (mode !== collaborationMode) onPick(mode);
    });
  };

  const modeTitle = (mode: CollaborationMode) => {
    switch (mode) {
      case "plan":
        return t("composer.modePlan");
      case "ask":
        return t("composer.collabAsk");
      case "goal":
        return t("composer.modeGoal");
      default:
        return t("composer.modeAgent");
    }
  };

  const modeDesc = (mode: CollaborationMode) => {
    switch (mode) {
      case "plan":
        return t("composer.planModeDesc");
      case "ask":
        return t("composer.collabAskDesc");
      case "goal":
        return hasGoal && collaborationMode === "goal"
          ? (goal ?? t("composer.goalModeActiveDesc"))
          : t("composer.goalModeDesc");
      default:
        return t("composer.modeAgentDesc");
    }
  };

  return (
    <div className="modelsw modesw">
      <button
        ref={triggerRef}
        type="button"
        className={`modelsw__trigger modesw__trigger${triggerMuted ? "" : " modesw__trigger--explicit"}`}
        disabled={disabled || running}
        aria-expanded={open && !closing}
        aria-haspopup="menu"
        aria-label={t("composer.modeMenuTitle")}
        title={open || closing ? undefined : t("composer.modeMenuTitle")}
        onClick={() => (open || closing ? closeMenu() : openMenu())}
      >
        <span className="modelsw__kind modesw__kind" aria-hidden="true">{triggerIcon}</span>
        <span className="modelsw__label">{current}</span>
        <ChevronsUpDown size={11} />
      </button>
      <AnchoredPopover
        open={open && !disabled && !running}
        closing={closing}
        anchorRef={triggerRef}
        onClose={() => closeMenu()}
        className="composer-access-menu composer-intent-menu modesw__menu"
        align="start"
      >
        <div className="composer-access-menu__section">
          <div className="composer-access-menu__label">{t("composer.modeMenuTitle")}</div>
          {MODE_OPTIONS.map((mode) => {
            const active = mode === collaborationMode;
            return (
              <button
                key={mode}
                type="button"
                className={`composer-access-menu__item composer-intent-menu__item${active ? " composer-access-menu__item--active" : ""}`}
                onClick={() => pick(mode)}
              >
                {modeIcon(mode)}
                <span className="composer-access-menu__copy">
                  <span className="composer-access-menu__title">{modeTitle(mode)}</span>
                  <span className="composer-access-menu__desc">{modeDesc(mode)}</span>
                </span>
                {active && <Check size={14} />}
              </button>
            );
          })}
        </div>
      </AnchoredPopover>
    </div>
  );
}
