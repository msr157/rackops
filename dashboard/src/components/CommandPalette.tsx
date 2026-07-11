import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  LayoutGrid, Boxes, HardDrive, Network, Settings, Rocket, Plus, Bot, Search,
  User, Building2, LifeBuoy, BookOpen,
} from 'lucide-react';
import { useData } from '../data';
import { Led } from './ui';

interface Entry {
  key: string;
  label: string;
  icon: React.ReactNode;
  hint?: string;
  run: () => void;
}

// CommandPalette: ⌘K / Ctrl+K jump-anywhere. Static actions first, then
// live apps and machines matched by name.
export function CommandPalette({ open, onClose, onAddMachine, onCopilot }: {
  open: boolean; onClose: () => void; onAddMachine: () => void; onCopilot: () => void;
}) {
  const navigate = useNavigate();
  const { services, nodes } = useData();
  const [q, setQ] = useState('');
  const [hl, setHl] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (open) { setQ(''); setHl(0); setTimeout(() => inputRef.current?.focus(), 0); }
  }, [open]);

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
      { key: 'orgs', label: 'Organizations', icon: <Building2 size={14} />, hint: 'page', run: go('/organizations') },
      { key: 'support', label: 'Support tickets', icon: <LifeBuoy size={14} />, hint: 'page', run: go('/support') },
      { key: 'profile', label: 'Profile & API keys', icon: <User size={14} />, hint: 'page', run: go('/profile') },
      { key: 'settings', label: 'Settings', icon: <Settings size={14} />, hint: 'page', run: go('/settings') },
      { key: 'swagger', label: 'API docs (Swagger)', icon: <BookOpen size={14} />, hint: 'docs', run: () => { window.open('/swagger', '_blank'); onClose(); } },
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

  if (!open) return null;

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setHl((h) => Math.min(h + 1, entries.length - 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setHl((h) => Math.max(h - 1, 0)); }
    else if (e.key === 'Enter') { e.preventDefault(); entries[hl]?.run(); }
    else if (e.key === 'Escape') onClose();
    // keep the highlighted row in view
    setTimeout(() => listRef.current?.querySelector('.hl')?.scrollIntoView({ block: 'nearest' }), 0);
  };

  return (
    <div className="palette-overlay" onClick={onClose}>
      <div className="palette" onClick={(e) => e.stopPropagation()}>
        <div className="row" style={{ gap: 0 }}>
          <Search size={15} style={{ marginLeft: 16, color: 'var(--faint)', flex: 'none' }} />
          <input ref={inputRef} placeholder="Jump to an app, machine, or action…"
            value={q} onChange={(e) => { setQ(e.target.value); setHl(0); }} onKeyDown={onKey} />
        </div>
        <div className="palette-list" ref={listRef}>
          {entries.map((e, i) => (
            <button key={e.key} className={`palette-item ${i === hl ? 'hl' : ''}`}
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
