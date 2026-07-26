import { createContext, useCallback, useContext, useId, useRef, useState } from 'react';
import { AlertTriangle } from 'lucide-react';
import { useDialogBehavior } from './useDialog';

interface ConfirmOpts {
  title: string;
  body: string;
  confirmLabel?: string;
  danger?: boolean;
}

type ConfirmFn = (opts: ConfirmOpts) => Promise<boolean>;

const ConfirmCtx = createContext<ConfirmFn | null>(null);

export function useConfirm(): ConfirmFn {
  const c = useContext(ConfirmCtx);
  if (!c) throw new Error('useConfirm outside ConfirmProvider');
  return c;
}

// ConfirmProvider renders one promise-based dialog for the whole app, so
// destructive actions always pause for an explicit decision.
export function ConfirmProvider({ children }: { children: React.ReactNode }) {
  const [opts, setOpts] = useState<ConfirmOpts | null>(null);
  const resolver = useRef<(v: boolean) => void>(() => {});

  const confirm = useCallback<ConfirmFn>((o) => {
    setOpts(o);
    return new Promise<boolean>((resolve) => { resolver.current = resolve; });
  }, []);

  const close = (v: boolean) => {
    resolver.current(v);
    setOpts(null);
  };

  return (
    <ConfirmCtx.Provider value={confirm}>
      {children}
      {opts && <ConfirmDialog opts={opts} onResolve={close} />}
    </ConfirmCtx.Provider>
  );
}

// Its own component so the dialog's focus trap mounts and unmounts with the
// dialog, not with the provider. Escape and the overlay both mean "cancel".
function ConfirmDialog({ opts, onResolve }: { opts: ConfirmOpts; onResolve: (v: boolean) => void }) {
  const ref = useDialogBehavior<HTMLDivElement>(() => onResolve(false));
  const titleId = useId();
  const bodyId = useId();
  return (
    <div className="modal-overlay" onClick={() => onResolve(false)}>
      <div ref={ref} className="modal" tabIndex={-1} style={{ width: 420 }}
        onClick={(e) => e.stopPropagation()}
        role="alertdialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={bodyId}>
        <h2 id={titleId} className="row" style={{ gap: 8 }}>
          {opts.danger && <AlertTriangle size={16} style={{ color: 'var(--crit)' }} />}
          {opts.title}
        </h2>
        <p id={bodyId} className="dim" style={{ marginBottom: 20, lineHeight: 1.6 }}>{opts.body}</p>
        <div className="row" style={{ justifyContent: 'flex-end' }}>
          {/* Cancel is first in the DOM, so the focus trap lands on it. */}
          <button className="btn" onClick={() => onResolve(false)}>Cancel</button>
          <button className={`btn ${opts.danger ? 'danger' : 'primary'}`} onClick={() => onResolve(true)}>
            {opts.confirmLabel ?? 'Confirm'}
          </button>
        </div>
      </div>
    </div>
  );
}
