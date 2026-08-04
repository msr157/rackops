import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { Check, Copy, HardDrive, RotateCw } from 'lucide-react';
import { api } from '../api';
import { useData } from '../data';
import { cleanError } from './toast';
import { CopyBtn, Modal, Spinner } from './ui';

type JoinState =
  | { phase: 'loading' }
  | { phase: 'ready'; command: string }
  | { phase: 'error'; message: string };

// AddMachineModal is command-first: one instruction, the join command with a
// Copy button, one caption about the token. When the machine reports in, the
// modal says so and Done turns primary.
export function AddMachineModal({ onClose }: { onClose: () => void }) {
  const { nodes } = useData();
  const [state, setState] = useState<JoinState>({ phase: 'loading' });

  // Machines already in the fleet when the modal opened. Anything beyond
  // this set joined while the operator was looking at the command.
  const [baseline] = useState(() => new Set(nodes.map((n) => n.id)));
  const joined = nodes.filter((n) => !baseline.has(n.id));

  const load = useCallback(() => {
    setState({ phase: 'loading' });
    api.joinCommand()
      .then((r) => setState({ phase: 'ready', command: r.command }))
      .catch((e) => {
        const msg = cleanError(e);
        setState({
          phase: 'error',
          message: /failed to fetch|networkerror|load failed/i.test(msg)
            ? 'The server didn’t answer. Is the control plane running?'
            : msg,
        });
      });
  }, []);

  useEffect(() => { load(); }, [load]);

  return (
    <Modal title="Add a machine" icon={<HardDrive size={16} />} onClose={onClose}>
      <p className="dim" style={{ marginBottom: 14, lineHeight: 1.6 }}>
        Copy the <code className="mono">rackops</code> binary to the new machine, then run
        this command on it. The machine joins the rack within seconds.
      </p>

      {state.phase === 'loading' && (
        <div className="row" style={{ gap: 8 }}>
          <code className="code-block row" style={{ flex: 1, gap: 8 }}>
            <Spinner size={13} /> <span className="dim">fetching a join command…</span>
          </code>
          <button className="btn sm" disabled><Copy size={12} /> Copy</button>
        </div>
      )}
      {state.phase === 'ready' && (
        <>
          <div className="row" style={{ gap: 8 }}>
            <code className="code-block" style={{ flex: 1 }}>{state.command}</code>
            <CopyBtn text={state.command} label="Copy" />
          </div>
          <div className="row" style={{ marginTop: 10, alignItems: 'flex-start' }}>
            <span className="faint" style={{ flex: 1, lineHeight: 1.6 }}>
              The command carries a join token that authorizes the machine, and the token
              expires, so run it soon.
            </span>
            <button className="btn sm ghost" style={{ flex: 'none' }} onClick={load}>
              <RotateCw size={12} aria-hidden /> New command
            </button>
          </div>
        </>
      )}
      {state.phase === 'error' && (
        <div className="auth-notice err" role="alert" style={{ marginBottom: 0 }}>
          <div style={{ marginBottom: 8 }}>
            <strong>Couldn&rsquo;t get a join command.</strong> {state.message}
          </div>
          <button className="btn sm" onClick={load}><RotateCw size={12} aria-hidden /> Retry</button>
        </div>
      )}

      {joined.map((n) => (
        <div key={n.id} className="row" style={{ marginTop: 12 }} role="status">
          <span className="badge ok"><Check size={11} aria-hidden /> {n.name} joined</span>
          <Link to={`/machines/${n.id}`} onClick={onClose} style={{ fontSize: 12.5 }}>
            View machine
          </Link>
        </div>
      ))}

      <div className="row" style={{ justifyContent: 'flex-end', marginTop: 20 }}>
        <button className={`btn ${joined.length > 0 ? 'primary' : ''}`} onClick={onClose}>
          Done
        </button>
      </div>
    </Modal>
  );
}
