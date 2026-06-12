import { useCallback, useEffect, useRef, useState } from "react";
import { Check, ChevronsUpDown, Gauge } from "lucide-react";
import { useT } from "../lib/i18n";
import type { TokenMode } from "../lib/types";
import { ANCHORED_POPOVER_CLOSE_MS, AnchoredPopover } from "./AnchoredPopover";

const TOKEN_OPTIONS: TokenMode[] = ["full", "economy"];

export function TokenSwitcher({
  tokenMode,
  disabled,
  running,
  onPick,
}: {
  tokenMode: TokenMode;
  disabled: boolean;
  running: boolean;
  onPick: (mode: TokenMode) => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [closing, setClosing] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const closeTimerRef = useRef<number | null>(null);
  const economyOn = tokenMode === "economy";
  const current = economyOn ? t("composer.tokenEconomy") : t("composer.tokenModeFull");

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

  const pick = (mode: TokenMode) => {
    closeMenu(() => {
      if (mode !== tokenMode) onPick(mode);
    });
  };

  return (
    <div className="modelsw tokensw">
      <button
        ref={triggerRef}
        type="button"
        className={`modelsw__trigger tokensw__trigger${economyOn ? " tokensw__trigger--explicit" : ""}`}
        disabled={disabled || running}
        aria-expanded={open && !closing}
        aria-haspopup="menu"
        aria-label={t("composer.tokenEconomy")}
        title={open || closing ? undefined : t("composer.tokenEconomy")}
        onClick={() => (open || closing ? closeMenu() : openMenu())}
      >
        <Gauge size={13} className="modelsw__kind" />
        <span className="modelsw__label">{current}</span>
        <ChevronsUpDown size={11} />
      </button>
      <AnchoredPopover
        open={open && !disabled && !running}
        closing={closing}
        anchorRef={triggerRef}
        onClose={() => closeMenu()}
        className="composer-access-menu composer-intent-menu tokensw__menu"
        align="start"
      >
        <div className="composer-access-menu__section">
          <div className="composer-access-menu__label">{t("composer.tokenEconomy")}</div>
          {TOKEN_OPTIONS.map((mode) => {
            const active = mode === tokenMode;
            const title = mode === "economy" ? t("composer.tokenEconomy") : t("composer.tokenModeFull");
            const desc = mode === "economy" ? t("composer.tokenEconomyDesc") : t("composer.tokenModeFullDesc");
            return (
              <button
                key={mode}
                type="button"
                className={`composer-access-menu__item composer-intent-menu__item${active ? " composer-access-menu__item--active" : ""}`}
                onClick={() => pick(mode)}
              >
                <Gauge size={16} />
                <span className="composer-access-menu__copy">
                  <span className="composer-access-menu__title">{title}</span>
                  <span className="composer-access-menu__desc">{desc}</span>
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
