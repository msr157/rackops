import { useCallback, useEffect, useRef, useState } from 'react';
import { Building2, Check, ChevronsUpDown, Plus, Settings2 } from 'lucide-react';
import { Link } from 'react-router-dom';
import { api, getActiveOrg, setActiveOrg } from '../api';
import type { Organization, User } from '../api';
import { useToast, cleanError } from './toast';
import { Field, Modal, Skeleton, Spinner } from './ui';

// OrgSwitcher: the Dokploy-style dropdown at the top of the sidebar. The
// active org scopes everything the dashboard shows; switching reloads data
// through the X-Rackops-Org header.
export function OrgSwitcher({ me }: { me: User | null }) {
  const toast = useToast();
  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState('');
  const [nameError, setNameError] = useState('');
  const [busy, setBusy] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const retryTimer = useRef<number | undefined>(undefined);

  const load: () => Promise<void> = useCallback(() => {
    window.clearTimeout(retryTimer.current);
    return api.orgs().then((o) => {
      setOrgs(o ?? []);
      setLoaded(true);
      // First run / stale selection: fall back to the first org we belong to.
      if (o?.length && !o.some((x) => x.id === getActiveOrg())) setActiveOrg(o[0].id);
    }).catch(() => {
      // An outage must never read as "you have no organizations": keep
      // whatever we last knew (or the skeleton) and quietly try again.
      retryTimer.current = window.setTimeout(() => { load(); }, 5000);
    });
  }, []);

  useEffect(() => {
    load();
    return () => window.clearTimeout(retryTimer.current);
  }, [load]);

  // Anything that switches, creates, or deletes an org announces it through
  // this event — reflect it here so the switcher never shows a stale list.
  useEffect(() => {
    const onOrg = () => { load(); };
    window.addEventListener('rackops-org-changed', onOrg);
    return () => window.removeEventListener('rackops-org-changed', onOrg);
  }, [load]);

  // Close on outside click.
  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [open]);

  // Menu convention: opening moves focus to the checked item (or the first).
  useEffect(() => {
    if (!open) return;
    const items = menuRef.current?.querySelectorAll<HTMLElement>('.org-item');
    if (!items || items.length === 0) return;
    const checked = menuRef.current?.querySelector<HTMLElement>('.org-item[aria-checked="true"]');
    (checked ?? items[0]).focus();
  }, [open]);

  const active = orgs.find((o) => o.id === getActiveOrg()) ?? orgs[0];
  const platformAdmin = me?.role === 'owner' || me?.role === 'admin';

  const closeMenu = (refocus: boolean) => {
    setOpen(false);
    if (refocus) triggerRef.current?.focus();
  };

  // Real menu keyboard behavior, not just the ARIA costume: Escape closes and
  // hands focus back, arrows cycle the items, Tab leaves via the trigger.
  const onMenuKeys = (e: React.KeyboardEvent) => {
    if (!open) return;
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      closeMenu(true);
      return;
    }
    if (e.key === 'Tab') {
      // Refocus the trigger before the default Tab runs, so focus continues
      // from the trigger instead of falling to <body> when the menu unmounts.
      triggerRef.current?.focus();
      setOpen(false);
      return;
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp' && e.key !== 'Home' && e.key !== 'End') return;
    const items = Array.from(menuRef.current?.querySelectorAll<HTMLElement>('.org-item') ?? []);
    if (items.length === 0) return;
    e.preventDefault();
    const i = items.indexOf(document.activeElement as HTMLElement);
    const next = e.key === 'Home' ? 0
      : e.key === 'End' ? items.length - 1
      : e.key === 'ArrowDown' ? (i + 1) % items.length
      : i <= 0 ? items.length - 1 : i - 1;
    items[next]?.focus();
  };

  const openCreate = () => {
    setNewName('');
    setNameError('');
    setCreating(true);
  };

  const create = async () => {
    if (busy) return;
    const trimmed = newName.trim();
    if (!trimmed) { setNameError('Give the organization a name.'); return; }
    setBusy(true);
    try {
      const org = await api.createOrg(trimmed);
      toast.ok(`Organization "${org.name}" created.`);
      setCreating(false); setNewName('');
      await load();
      setActiveOrg(org.id);
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  return (
    <div className="org-switcher" ref={ref} onKeyDown={onMenuKeys}>
      {!loaded && orgs.length === 0 ? (
        // The list hasn't arrived yet — hold the trigger's exact footprint.
        <Skeleton height={35} radius="var(--r-md)" />
      ) : !active ? (
        // Zero organizations: a quiet way in, same footprint as the trigger.
        <button ref={triggerRef} className="org-trigger" onClick={openCreate}>
          <Plus size={14} style={{ color: 'var(--faint)', flexShrink: 0 }} aria-hidden />
          <span className="org-name nav-label" style={{ color: 'var(--dim)' }}>Create organization</span>
        </button>
      ) : (
        <button ref={triggerRef} className="org-trigger" onClick={() => setOpen((v) => !v)}
          title={active.name} aria-label={`Switch organization (current: ${active.name})`}
          aria-haspopup="menu" aria-expanded={open}
          onKeyDown={(e) => {
            if (!open && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) { e.preventDefault(); setOpen(true); }
          }}>
          <Building2 size={14} style={{ color: 'var(--accent)', flexShrink: 0 }} aria-hidden />
          <span className="org-name nav-label">{active.name}</span>
          <ChevronsUpDown size={12} style={{ marginLeft: 'auto', color: 'var(--faint)', flexShrink: 0 }} aria-hidden />
        </button>
      )}

      {open && active && (
        <div className="org-menu" role="menu" aria-label="Organizations" ref={menuRef}>
          <div className="org-menu-label" aria-hidden>Organizations</div>
          {/* A member of many orgs scrolls; the menu never outgrows the viewport. */}
          <div style={{ maxHeight: 240, overflowY: 'auto' }}>
            {orgs.map((o) => (
              <button key={o.id} className="org-item" role="menuitemradio" tabIndex={-1}
                aria-checked={o.id === active.id}
                onClick={() => { setActiveOrg(o.id); closeMenu(true); }}>
                <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{o.name}</span>
                {o.id === active.id && (
                  <Check size={12} style={{ marginLeft: 'auto', color: 'var(--accent)', flexShrink: 0 }} aria-hidden />
                )}
              </button>
            ))}
          </div>
          <div className="org-menu-sep" role="separator" />
          <Link to="/settings?tab=org" className="org-item" role="menuitem" tabIndex={-1}
            onClick={() => setOpen(false)}>
            <Settings2 size={12} aria-hidden /> Manage organization
          </Link>
          {platformAdmin && (
            <button className="org-item" role="menuitem" tabIndex={-1}
              onClick={() => { closeMenu(true); openCreate(); }}>
              <Plus size={12} aria-hidden /> New organization
            </button>
          )}
        </div>
      )}

      {creating && (
        <Modal title="New organization" icon={<Building2 size={15} />} width={380} onClose={() => setCreating(false)}>
          <div className="dim" style={{ marginBottom: 14 }}>
            An organization has its own projects, machines, domains, and members — a clean
            boundary between teams or clients.
          </div>
          <Field label="Name" error={nameError || undefined}>
            {(p) => (
              <input className="input" {...p} autoFocus value={newName} placeholder="e.g. Homelab, Acme Corp"
                onChange={(e) => { setNewName(e.target.value); if (nameError) setNameError(''); }}
                onKeyDown={(e) => e.key === 'Enter' && create()} />
            )}
          </Field>
          <button className="btn primary" style={{ width: '100%' }} disabled={busy} onClick={create}>
            {busy ? <Spinner size={13} /> : <Plus size={13} />} Create organization
          </button>
        </Modal>
      )}
    </div>
  );
}
