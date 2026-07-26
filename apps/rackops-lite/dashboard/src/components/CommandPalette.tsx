import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { LayoutGrid, Boxes, HardDrive, Network, Settings, Rocket, Plus, Bot, Search } from 'lucide-react';
import { useData } from '../data';
import { Led } from './ui';
import { useDialogBehavior } from './useDialog';

interface Entry {
  key: string;
  label: string;
  icon: React.ReactNode;
  hint?: string;
  run: () => void;
}

// CommandPalette: ⌘K / Ctrl+K jump-anywhere. Static actions first, then
// live apps and machines matched by name.
export function CommandPalette({ open, ...rest }: {
  open: boolean; onClose: () => void; onAddMachine: () => void; onCopilot: () => void;
}) {
  // Mounted only while open, so its focus trap and focus-restore run per
  // opening and every session starts on a cleared query.
  return open ? <Palette {...rest} /> : null;
}

function Palette({ onClose, onAddMachine, onCopilot }: {
  onClose: () => void; onAddMachine: () => void; onCopilot: () => void;
}) {
  const navigate = useNavigate();
  const { services, nodes } = useData();
  const [q, setQ] = useState('');
  const [hl, setHl] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);
  const ref = useDialogBehavior<HTMLDivElement>(onClose);
  const listId = useId();
  const optionId = (i: number) => `${listId}-opt-${i}`;

  const entries = useMemo<Entry[]>(() => {
    const go = (path: string) => () => { navigate(path); onClose(); };
    const staticEntries: Entry[] = [
      { key: 'deploy', label: 'Deploy an app', icon: <Rocket size={14} />, hint: 'action', run: go('/apps/new') },
      { key: 'add-machine', label: 'Add a machine', icon: <Plus size={14} />, hint: 'action', run: () => { onAddMachine(); onClose(); } },
      { key: 'copilot', label: 'Ask Copilot', icon: <Bot size={14} />, hint: 'action', run: () => { onCopilot(); onClose(); } },
      { key: 'home', label: 'Home', icon: <LayoutGrid size={14} />, hint: 'page', run: go('/') },
      { key: 'apps', label: 'Apps', icon: <Boxes size={14} />, hint: 'page', run: go('/apps') },
      { key: 'machines', label: 'Machines', icon: <HardDrive size={14} />, hint: 'page', run: go('/machines') },
      { key: 'network', label: 'Network', icon: <Network size={14} />, hint: 'page', run: go('/network') },
      { key: 'settings', label: 'Settings', icon: <Settings size={14} />, hint: 'page', run: go('/settings') },
    ];
    const appEntries: Entry[] = services.map((s) => ({
      key: `app-${s.id}`, label: s.name, icon: <Led status={s.status} />, hint: 'app',
      run: go(`/apps/${s.id}`),
    }));
    const nodeEntries: Entry[] = nodes.map((n) => ({
      key: `node-${n.id}`, label: n.name, icon: <Led status={n.status} />, hint: 'machine',
      run: go(`/machines/${n.id}`),
    }));
    const all = [...staticEntries, ...appEntries, ...nodeEntries];
    if (!q.trim()) return all;
    const needle = q.toLowerCase();
    return all.filter((e) => e.label.toLowerCase().includes(needle));
  }, [q, services, nodes, navigate, onClose, onAddMachine, onCopilot]);

  useEffect(() => { setHl((h) => Math.min(h, Math.max(entries.length - 1, 0))); }, [entries]);

  // Escape is handled by the dialog hook; this owns the list navigation.
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setHl((h) => Math.min(h + 1, entries.length - 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setHl((h) => Math.max(h - 1, 0)); }
    else if (e.key === 'Enter') { e.preventDefault(); entries[hl]?.run(); }
    else return;
    // keep the highlighted row in view
    setTimeout(() => listRef.current?.querySelector('.hl')?.scrollIntoView({ block: 'nearest' }), 0);
  };

  return (
    <div className="palette-overlay" onClick={onClose}>
      <div ref={ref} className="palette" tabIndex={-1} onClick={(e) => e.stopPropagation()}
        role="dialog" aria-modal="true" aria-label="Jump to">
        <div className="row" style={{ gap: 0 }}>
          <Search size={15} style={{ marginLeft: 16, color: 'var(--faint)', flex: 'none' }} aria-hidden />
          <input placeholder="Jump to an app, machine, or action…"
            aria-label="Jump to an app, machine, or action"
            role="combobox" aria-expanded aria-controls={listId} aria-autocomplete="list"
            aria-activedescendant={entries[hl] ? optionId(hl) : undefined}
            value={q} onChange={(e) => { setQ(e.target.value); setHl(0); }} onKeyDown={onKey} />
        </div>
        <div className="palette-list" ref={listRef} id={listId} role="listbox">
          {entries.map((e, i) => (
            <button key={e.key} id={optionId(i)} role="option" aria-selected={i === hl} tabIndex={-1}
              className={`palette-item ${i === hl ? 'hl' : ''}`}
              onMouseEnter={() => setHl(i)} onClick={e.run}>
              {e.icon} {e.label}
              {e.hint && <span className="hint">{e.hint}</span>}
            </button>
          ))}
          {entries.length === 0 && <div className="faint" style={{ padding: 14 }}>Nothing matches "{q}".</div>}
        </div>
      </div>
    </div>
  );
}
