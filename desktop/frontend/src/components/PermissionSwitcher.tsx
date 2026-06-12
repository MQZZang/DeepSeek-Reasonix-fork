import { useCallback, useEffect, useRef, useState } from "react";
import { Check, ChevronsUpDown, Shield, ShieldAlert, ShieldCheck } from "lucide-react";
import { useT, type Translator } from "../lib/i18n";
import type { ToolApprovalMode } from "../lib/types";
import { ANCHORED_POPOVER_CLOSE_MS, AnchoredPopover } from "./AnchoredPopover";

const PERMISSION_OPTIONS: ToolApprovalMode[] = ["ask", "auto", "yolo"];

function permissionIcon(mode: ToolApprovalMode, size = 16) {
  switch (mode) {
    case "ask":
      return <Shield size={size} />;
    case "yolo":
      return <ShieldAlert size={size} />;
    default:
      return <ShieldCheck size={size} />;
  }
}

function permissionLabel(mode: ToolApprovalMode, t: Translator): string {
  switch (mode) {
    case "ask":
      return t("composer.accessAsk");
    case "yolo":
      return t("composer.accessYolo");
    default:
      return t("composer.accessAuto");
  }
}

export function PermissionSwitcher({
  toolApprovalMode,
  disabled,
  onPick,
}: {
  toolApprovalMode: ToolApprovalMode;
  disabled: boolean;
  onPick: (mode: ToolApprovalMode) => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [closing, setClosing] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const closeTimerRef = useRef<number | null>(null);
  const current = permissionLabel(toolApprovalMode, t);
  const triggerIcon = permissionIcon(toolApprovalMode, 13);

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

  const permissionTitle = (mode: ToolApprovalMode) => {
    switch (mode) {
      case "ask":
        return t("composer.accessAsk");
      case "yolo":
        return t("composer.accessYolo");
      default:
        return t("composer.accessAuto");
    }
  };

  const permissionDesc = (mode: ToolApprovalMode) => {
    switch (mode) {
      case "ask":
        return t("composer.accessAskDesc");
      case "yolo":
        return t("composer.accessYoloDesc");
      default:
        return t("composer.accessAutoDesc");
    }
  };

  const pick = (mode: ToolApprovalMode) => {
    closeMenu(() => {
      if (mode !== toolApprovalMode) onPick(mode);
    });
  };

  return (
    <div className="modelsw permissionsw">
      <button
        ref={triggerRef}
        type="button"
        className={`modelsw__trigger permissionsw__trigger${toolApprovalMode === "auto" ? "" : " permissionsw__trigger--explicit"}`}
        disabled={disabled}
        aria-expanded={open && !closing}
        aria-haspopup="menu"
        aria-label={t("composer.permissionMenuTitle")}
        title={open || closing ? undefined : t("composer.permissionMenuTitle")}
        onClick={() => (open || closing ? closeMenu() : openMenu())}
      >
        <span className="modelsw__kind permissionsw__kind" aria-hidden="true">{triggerIcon}</span>
        <span className="modelsw__label">{current}</span>
        <ChevronsUpDown size={11} />
      </button>
      <AnchoredPopover
        open={open && !disabled}
        closing={closing}
        anchorRef={triggerRef}
        onClose={() => closeMenu()}
        className="composer-access-menu composer-intent-menu permissionsw__menu"
        align="start"
      >
        <div className="composer-access-menu__section">
          <div className="composer-access-menu__label">{t("composer.permissionMenuTitle")}</div>
          {PERMISSION_OPTIONS.map((mode) => {
            const active = mode === toolApprovalMode;
            return (
              <button
                key={mode}
                type="button"
                className={`composer-access-menu__item composer-intent-menu__item${active ? " composer-access-menu__item--active" : ""}`}
                onClick={() => pick(mode)}
              >
                {permissionIcon(mode)}
                <span className="composer-access-menu__copy">
                  <span className="composer-access-menu__title">{permissionTitle(mode)}</span>
                  <span className="composer-access-menu__desc">{permissionDesc(mode)}</span>
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
