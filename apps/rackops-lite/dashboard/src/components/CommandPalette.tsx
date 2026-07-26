import { Fragment, useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  LayoutGrid, Boxes, HardDrive, Network, Settings, Rocket, Plus, Bot, Search,
  Play, Square, RotateCw, ScrollText,
} from 'lucide-react';
import { api } from '../api';
import type { Service } from '../api';
import { useData } from '../data';
import { useConfirm } from './confirm';
import { cleanError, useToast } from './toast';
import { Led } from './ui';
import { useDialogBehavior } from './useDialog';

type Group = 'Actions' | 'Pages' | 'Apps' | 'Machines';

interface Entry {
  key: string;
  group: Group;
  label: string;
  icon: React.ReactNode;
  /** Right-aligned mono hint: an app's status, a machine's IP. */
  hint?: string;
  /** Extra text the search matches beyond the label (status, IP). */
  match?: string;
  run: () => void;
}

// CommandPalette: ⌘K / Ctrl+K acts, not just navigates. Opened empty it is a
// directory (Pages, the three global actions, Apps, Machines); typing brings
// in per-app lifecycle verbs, searchable by name, status, or (for machines)
// IP.
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
  const { services, nodes, refresh, loaded } = useData();
  const toast = useToast();
  const confirm = useConfirm();
  const [q, setQ] = useState('');
  const [hl, setHl] = useState(0);
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);
  // Scrolling the list under a resting cursor fires mouseenter on whatever
  // lands beneath it; only a real mouse move may steal the highlight.
  const mouseMoved = useRef(false);
  const ref = useDialogBehavior<HTMLDivElement>(onClose);
  const listId = useId();
  const optionId = (i: number) => `${listId}-opt-${i}`;

  // Fire a lifecycle action and get out of the way: the palette closes
  // immediately, the toast reports the outcome. Same call, same success and
  // error copy as the buttons on the Apps page. Stop pauses for a routine
  // confirm (Enter accepts); start and restart never confirm.
  const act = useCallback(async (s: Service, action: 'start' | 'stop' | 'restart') => {
    onClose();
    if (action === 'stop') {
      const ok = await confirm({
        title: `Stop ${s.name}?`,
        body: `${s.name} stops serving until you start it again.`,
        confirmLabel: 'Stop app',
      });
      if (!ok) return;
    }
    api.serviceAction(s.id, action)
      .then(() => {
        toast.ok(action === 'stop' ? `${s.name} stopping.` : action === 'start' ? `${s.name} starting.` : `${s.name} restarting.`);
        refresh();
      })
      .catch((err) => toast.err(cleanError(err)));
  }, [onClose, confirm, refresh, toast]);

  const entries = useMemo<Entry[]>(() => {
    const go = (path: string) => () => { navigate(path); onClose(); };

    const pages: Entry[] = [
      { key: 'home', group: 'Pages', label: 'Home', icon: <LayoutGrid size={14} />, run: go('/') },
      { key: 'apps', group: 'Pages', label: 'Apps', icon: <Boxes size={14} />, run: go('/apps') },
      { key: 'machines', group: 'Pages', label: 'Machines', icon: <HardDrive size={14} />, run: go('/machines') },
      { key: 'network', group: 'Pages', label: 'Network', icon: <Network size={14} />, run: go('/network') },
      { key: 'settings', group: 'Pages', label: 'Settings', icon: <Settings size={14} />, run: go('/settings') },
    ];

    const actions: Entry[] = [
      { key: 'deploy', group: 'Actions', label: 'Deploy an app', icon: <Rocket size={14} />, run: go('/apps/new') },
      { key: 'add-machine', group: 'Actions', label: 'Add a machine', icon: <Plus size={14} />, run: () => { onAddMachine(); onClose(); } },
      { key: 'copilot', group: 'Actions', label: 'Ask Copilot', icon: <Bot size={14} />, run: () => { onCopilot(); onClose(); } },
    ];

    // Per-service lifecycle verbs join the Actions group only once the query
    // is non-empty; the resting palette stays a short directory instead of a
    // wall of every verb for every app.
    const lifecycle: Entry[] = [];
    for (const s of services) {
      // Mirrors the Apps page: no start/stop/restart while a deploy or
      // restart is already in flight; the server would refuse it anyway.
      const working = s.status === 'building' || s.status === 'pending';
      if (!working) {
        if (s.status === 'running') {
          lifecycle.push({
            key: `stop-${s.id}`, group: 'Actions', label: `Stop ${s.name}`,
            icon: <Square size={14} />, match: s.status, run: () => { void act(s, 'stop'); },
          });
        } else {
          lifecycle.push({
            key: `start-${s.id}`, group: 'Actions', label: `Start ${s.name}`,
            icon: <Play size={14} />, match: s.status, run: () => { void act(s, 'start'); },
          });
        }
        lifecycle.push({
          key: `restart-${s.id}`, group: 'Actions', label: `Restart ${s.name}`,
          icon: <RotateCw size={14} />, match: s.status, run: () => { void act(s, 'restart'); },
        });
      }
      lifecycle.push({
        key: `logs-${s.id}`, group: 'Actions', label: `Logs: ${s.name}`,
        icon: <ScrollText size={14} />, match: s.status, run: go(`/apps/${s.id}/logs`),
      });
    }

    // The status is the visible hint, so the dot needs no sr-only echo here.
    const appEntries: Entry[] = services.map((s) => ({
      key: `app-${s.id}`, group: 'Apps', label: s.name, icon: <Led status={s.status} />,
      hint: s.status, match: s.status, run: go(`/apps/${s.id}`),
    }));

    const nodeEntries: Entry[] = nodes.map((n) => ({
      key: `node-${n.id}`, group: 'Machines', label: n.name, icon: <Led status={n.status} label />,
      hint: n.ip, match: `${n.status} ${n.ip}`, run: go(`/machines/${n.id}`),
    }));

    const needle = q.trim().toLowerCase();
    if (!needle) return [...pages, ...actions, ...appEntries, ...nodeEntries];
    const all = [...pages, ...actions, ...lifecycle, ...appEntries, ...nodeEntries];
    return all.filter((e) => `${e.label} ${e.match ?? ''}`.toLowerCase().includes(needle));
  }, [q, services, nodes, navigate, onClose, onAddMachine, onCopilot, act]);

  useEffect(() => { setHl((h) => Math.min(h, Math.max(entries.length - 1, 0))); }, [entries]);

  // Keep the highlighted row in view, driven by the row's own ref, not a
  // class-selector query racing the next paint.
  useEffect(() => {
    itemRefs.current[hl]?.scrollIntoView({ block: 'nearest' });
  }, [hl, entries]);

  // Escape is handled by the dialog hook; this owns the list navigation.
  // Arrows wrap first↔last, Home/End jump.
  const onKey = (e: React.KeyboardEvent) => {
    const n = entries.length;
    if (e.key === 'ArrowDown') { e.preventDefault(); if (n > 0) setHl((h) => (h + 1) % n); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); if (n > 0) setHl((h) => (h - 1 + n) % n); }
    else if (e.key === 'Home') { e.preventDefault(); setHl(0); }
    else if (e.key === 'End') { e.preventDefault(); setHl(Math.max(n - 1, 0)); }
    else if (e.key === 'Enter') { e.preventDefault(); entries[hl]?.run(); return; }
    else return;
    // Keyboard moved the list; a stationary cursor may not steal it back.
    mouseMoved.current = false;
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
        <div className="palette-list" id={listId} role="listbox"
          onMouseMove={() => { mouseMoved.current = true; }}>
          {entries.map((e, i) => (
            <Fragment key={e.key}>
              {(i === 0 || entries[i - 1].group !== e.group) && (
                <div className="palette-group" role="presentation">{e.group}</div>
              )}
              <button id={optionId(i)} role="option" aria-selected={i === hl} tabIndex={-1}
                ref={(el) => { itemRefs.current[i] = el; }}
                className={`palette-item ${i === hl ? 'hl' : ''}`}
                onMouseEnter={() => { if (mouseMoved.current) setHl(i); }}
                onClick={e.run}>
                {e.icon} {e.label}
                {e.hint && <span className="hint">{e.hint}</span>}
              </button>
            </Fragment>
          ))}
          {entries.length === 0 && (
            <div className="palette-empty">
              {loaded
                ? <>Nothing matches &ldquo;{q}&rdquo;. Try an app name, a status like failed, or a machine IP.</>
                : 'Still reading the fleet. Try again in a second.'}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
