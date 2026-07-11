import { useEffect, useState } from 'react';
import { HardDrive } from 'lucide-react';
import { api } from '../api';
import { Modal, CopyField } from './ui';

// AddMachineModal shows the one-line join command. The machine appears in
// the fleet on its own within seconds of running it.
export function AddMachineModal({ onClose }: { onClose: () => void }) {
  const [command, setCommand] = useState('');

  useEffect(() => {
    api.joinCommand().then((r) => setCommand(r.command)).catch(() => setCommand('server unreachable — is the control plane running?'));
  }, []);

  return (
    <Modal title="Add a machine" icon={<HardDrive size={16} />} onClose={onClose}>
      <p className="dim" style={{ marginBottom: 14, lineHeight: 1.6 }}>
        Copy the <code className="mono">rackops</code> binary to the new machine, then run this
        command on it. The machine joins the rack within seconds and its hardware is
        discovered automatically.
      </p>
      <CopyField text={command} />
      <p className="faint" style={{ marginTop: 14, lineHeight: 1.6 }}>
        Works behind CGNAT and firewalls — the agent only makes outbound connections.
        No port forwarding, no VPN setup.
      </p>
    </Modal>
  );
}
