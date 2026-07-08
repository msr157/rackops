import { useEffect, useState } from 'react';
import { Server, Copy, Check, X } from 'lucide-react';
import { api } from '../api';

export function OnboardModal({ onClose }: { onClose: () => void }) {
  const [command, setCommand] = useState('');
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    api.joinCommand().then((r) => setCommand(r.command)).catch(() => setCommand('server unreachable'));
  }, []);

  const copy = async () => {
    await navigator.clipboard.writeText(command);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="row spread" style={{ marginBottom: 16 }}>
          <h2 style={{ margin: 0 }}><Server size={16} style={{ verticalAlign: -2 }} /> Onboard a Node</h2>
          <button className="btn sm" onClick={onClose}><X size={13} /></button>
        </div>

        <p className="dim" style={{ marginBottom: 14, lineHeight: 1.6 }}>
          Copy the <code className="mono">rackops</code> binary to the new machine, then run this command.
          The node will appear in the fleet within seconds — hardware discovered automatically.
        </p>

        <div className="code-block" style={{ marginBottom: 14 }}>{command || 'loading…'}</div>

        <button className="btn primary" style={{ width: '100%', justifyContent: 'center' }} onClick={copy}>
          {copied ? <><Check size={14} /> Copied!</> : <><Copy size={14} /> Copy command</>}
        </button>

        <p className="faint" style={{ marginTop: 14, lineHeight: 1.6 }}>
          Works behind CGNAT and firewalls — the agent only makes outbound connections.
          No port forwarding, no VPN setup, no kubectl.
        </p>
      </div>
    </div>
  );
}
