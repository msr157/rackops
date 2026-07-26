import { createContext, useCallback, useContext, useRef, useState } from 'react';
import { CheckCircle2, AlertTriangle, Info, X } from 'lucide-react';

type Kind = 'ok' | 'err' | 'info';
interface Toast { id: number; kind: Kind; msg: string }

interface ToastApi {
  ok: (msg: string) => void;
  err: (msg: string) => void;
  info: (msg: string) => void;
}

const ToastCtx = createContext<ToastApi | null>(null);

export function useToast(): ToastApi {
  const t = useContext(ToastCtx);
  if (!t) throw new Error('useToast outside ToastProvider');
  return t;
}

// cleanError strips the "Error: 409: " transport prefix off API errors so
// toasts read as sentences, not stack traces.
export function cleanError(e: unknown): string {
  return String(e).replace(/^Error:\s*(\d+:\s*)?/, '').trim() || 'Something went wrong.';
}

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextId = useRef(1);

  const dismiss = useCallback((id: number) => {
    setToasts((ts) => ts.filter((t) => t.id !== id));
  }, []);

  // Errors stay until dismissed — the one message a user needs to re-read is
  // the one explaining what just failed. Confirmations time out as usual.
  const push = useCallback((kind: Kind, msg: string) => {
    const id = nextId.current++;
    setToasts((ts) => [...ts.slice(-4), { id, kind, msg }]);
    if (kind !== 'err') setTimeout(() => dismiss(id), 4200);
  }, [dismiss]);

  const apiRef = useRef<ToastApi>({
    ok: (m) => push('ok', m),
    err: (m) => push('err', m),
    info: (m) => push('info', m),
  });
  // push is stable, so the api object never changes identity.
  apiRef.current.ok = (m) => push('ok', m);
  apiRef.current.err = (m) => push('err', m);
  apiRef.current.info = (m) => push('info', m);

  const icons: Record<Kind, React.ReactNode> = {
    ok: <CheckCircle2 size={15} style={{ color: 'var(--ok)', flex: 'none', marginTop: 1 }} />,
    err: <AlertTriangle size={15} style={{ color: 'var(--crit)', flex: 'none', marginTop: 1 }} />,
    info: <Info size={15} style={{ color: 'var(--amber)', flex: 'none', marginTop: 1 }} />,
  };

  return (
    <ToastCtx.Provider value={apiRef.current}>
      {children}
      <div className="toast-stack" role="status" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={`toast ${t.kind}`}>
            {icons[t.kind]}
            <span style={{ flex: 1 }}>{t.msg}</span>
            <button className="toast-close" onClick={() => dismiss(t.id)} aria-label="Dismiss">
              <X size={13} />
            </button>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  );
}
