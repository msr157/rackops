import { createContext, useCallback, useContext, useRef, useState } from 'react';
import { AlertTriangle } from 'lucide-react';

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
      {opts && (
        <div className="modal-overlay" onClick={() => close(false)}>
          <div className="modal" style={{ width: 420 }} onClick={(e) => e.stopPropagation()} role="alertdialog" aria-modal="true">
            <h2 className="row" style={{ gap: 8 }}>
              {opts.danger && <AlertTriangle size={16} style={{ color: 'var(--crit)' }} />}
              {opts.title}
            </h2>
            <p className="dim" style={{ marginBottom: 20, lineHeight: 1.6 }}>{opts.body}</p>
            <div className="row" style={{ justifyContent: 'flex-end' }}>
              <button className="btn" onClick={() => close(false)} autoFocus>Cancel</button>
              <button className={`btn ${opts.danger ? 'danger' : 'primary'}`} onClick={() => close(true)}>
                {opts.confirmLabel ?? 'Confirm'}
              </button>
            </div>
          </div>
        </div>
      )}
    </ConfirmCtx.Provider>
  );
}
