import { useEffect, useRef } from 'react';

const FOCUSABLE = [
  'a[href]', 'button:not([disabled])', 'input:not([disabled])', 'select:not([disabled])',
  'textarea:not([disabled])', '[tabindex]:not([tabindex="-1"])',
].join(',');

// Only the topmost open dialog reacts to Escape and traps Tab, so a confirm
// raised from inside a modal closes itself and leaves the modal standing.
const stack: object[] = [];

function visibleFocusable(box: HTMLElement): HTMLElement[] {
  return [...box.querySelectorAll<HTMLElement>(FOCUSABLE)]
    .filter((el) => el.offsetWidth > 0 || el.offsetHeight > 0 || el === document.activeElement);
}

// useDialogBehavior gives an overlay the three things a keyboard user is owed:
// Escape closes it, Tab cycles inside it, and focus returns to whatever opened
// it. Attach the returned ref to the dialog's outermost box, and mount the
// component only while the dialog is open.
export function useDialogBehavior<T extends HTMLElement>(
  onClose: () => void,
  opts?: { initialFocus?: 'first' | 'container' },
) {
  const ref = useRef<T>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  const initialFocus = opts?.initialFocus ?? 'first';

  useEffect(() => {
    const token = {};
    stack.push(token);
    const opener = document.activeElement as HTMLElement | null;
    const box = ref.current;

    if (box) {
      // A control that asked for focus itself (autoFocus) keeps it — the
      // dialog only places focus when nothing inside has claimed it.
      const claimed = box.contains(document.activeElement) && document.activeElement !== box;
      if (!claimed) {
        if (initialFocus === 'container') box.focus();
        else (visibleFocusable(box)[0] ?? box).focus();
      }
    }

    const onKey = (e: KeyboardEvent) => {
      if (stack[stack.length - 1] !== token) return;
      if (e.key === 'Escape') { e.preventDefault(); closeRef.current(); return; }
      if (e.key !== 'Tab' || !ref.current) return;

      const items = visibleFocusable(ref.current);
      if (items.length === 0) { e.preventDefault(); ref.current.focus(); return; }
      const first = items[0];
      const last = items[items.length - 1];
      const active = document.activeElement;
      if (!ref.current.contains(active)) {
        e.preventDefault();
        (e.shiftKey ? last : first).focus();
      } else if (e.shiftKey && active === first) {
        e.preventDefault(); last.focus();
      } else if (!e.shiftKey && active === last) {
        e.preventDefault(); first.focus();
      }
    };

    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('keydown', onKey);
      const i = stack.indexOf(token);
      if (i !== -1) stack.splice(i, 1);
      // Hand focus back unless something outside the dialog already claimed it
      // (React may run this before or after the dialog leaves the DOM).
      const active = document.activeElement;
      if (!active || active === document.body || box?.contains(active)) opener?.focus?.();
    };
  }, [initialFocus]);

  return ref;
}
