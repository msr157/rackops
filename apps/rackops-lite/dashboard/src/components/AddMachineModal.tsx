import { useCallback, useEffect, useState } from 'react';
import { Check, Copy, HardDrive, RotateCw } from 'lucide-react';
import { api } from '../api';
import { useData } from '../data';
import { cleanError } from './toast';
import { CopyField, InfoTip, Modal, Spinner } from './ui';

type JoinState =
  | { phase: 'loading' }
  | { phase: 'ready'; command: string }
  | { phase: 'error'; message: string };

// AddMachineModal shows the one-line join command. The machine appears in
// the fleet on its own within seconds of running it — and this modal says so
// the moment it does.
export function AddMachineModal({ onClose }: { onClose: () => void }) {
  const { nodes } = useData();
  const [state, setState] = useState<JoinState>({ phase: 'loading' });

  // Machines already in the fleet when the modal opened — anything beyond
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
            ? 'The server didn’t answer — is the control plane running?'
            : msg,
        });
      });
  }, []);

  useEffect(() => { load(); }, [load]);

  return (
    <Modal title="Add a machine" icon={<HardDrive size={16} />} onClose={onClose}>
      <p className="dim" style={{ marginBottom: 14, lineHeight: 1.6 }}>
        Copy the <code className="mono">rackops</code> binary to the new machine, then run this
        command on it. The machine joins the rack within seconds and its hardware is
        discovered automatically.
        <InfoTip wide>
          The command carries a join token (which authorizes the machine) and your organization,
          so the new machine lands in the right place. The agent reports telemetry every 2 seconds
          and receives work over the same connection.
        </InfoTip>
      </p>

      {state.phase === 'loading' && (
        <div className="row" style={{ gap: 8 }}>
          <code className="code-block row" style={{ flex: 1, gap: 8 }}>
            <Spinner size={13} /> <span className="dim">fetching a join command…</span>
          </code>
          <button className="btn sm" disabled title="Copy" aria-label="Copy join command">
            <Copy size={12} />
          </button>
        </div>
      )}
      {state.phase === 'ready' && <CopyField text={state.command} />}
      {state.phase === 'error' && (
        <div className="auth-notice err" role="alert" style={{ marginBottom: 0 }}>
          <div style={{ marginBottom: 8 }}>
            <strong>Couldn&rsquo;t get a join command.</strong> {state.message}
          </div>
          <button className="btn sm" onClick={load}><RotateCw size={12} /> Retry</button>
        </div>
      )}

      <p className="faint" style={{ marginTop: 10, lineHeight: 1.6 }}>
        The command embeds a join token — it&rsquo;s what authorizes the machine, and it
        expires, so run it soon rather than saving it for later.
      </p>

      {joined.map((n) => (
        <div key={n.id} className="row" style={{ marginTop: 10 }} role="status">
          <span className="badge ok"><Check size={11} aria-hidden /> {n.name} joined</span>
          <span className="faint">it&rsquo;s reporting telemetry — you can close this window</span>
        </div>
      ))}

      <p className="faint" style={{ marginTop: 14, lineHeight: 1.6 }}>
        Works behind CGNAT and firewalls — the agent only makes outbound connections.
        No port forwarding, no VPN setup.
      </p>
    </Modal>
  );
}
