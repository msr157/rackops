import type { NodeStatus } from '@rackops/api';
import './StatusDot.css';

export function StatusDot({ status }: { status: NodeStatus }) {
  return (
    <span className={`ro-status ro-status-${status}`} title={status}>
      <span className="ro-status-dot" />
      {status}
    </span>
  );
}
