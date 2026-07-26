import { useEffect, useRef, useState } from 'react';
import { Building2, Check, ChevronsUpDown, Plus, Settings2 } from 'lucide-react';
import { Link } from 'react-router-dom';
import { api, getActiveOrg, setActiveOrg } from '../api';
import type { Organization, User } from '../api';
import { useToast, cleanError } from './toast';
import { Field, Modal } from './ui';

// OrgSwitcher: the Dokploy-style dropdown at the top of the sidebar. The
// active org scopes everything the dashboard shows; switching reloads data
// through the X-Rackops-Org header.
export function OrgSwitcher({ me }: { me: User | null }) {
  const toast = useToast();
  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState('');
  const ref = useRef<HTMLDivElement>(null);

  const load = () => api.orgs().then((o) => {
    setOrgs(o ?? []);
    // First run / stale selection: fall back to the first org we belong to.
    if (o?.length && !o.some((x) => x.id === getActiveOrg())) setActiveOrg(o[0].id);
  }).catch(() => {});
  useEffect(() => { load(); }, []);

  // Close on outside click.
  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [open]);

  const active = orgs.find((o) => o.id === getActiveOrg()) ?? orgs[0];
  const platformAdmin = me?.role === 'owner' || me?.role === 'admin';
  if (!active) return null;

  const create = async () => {
    if (!newName.trim()) return;
    try {
      const org = await api.createOrg(newName.trim());
      toast.ok(`Organization "${org.name}" created.`);
      setCreating(false); setNewName('');
      await load();
      setActiveOrg(org.id);
    } catch (e) { toast.err(cleanError(e)); }
  };

  return (
    <div className="org-switcher" ref={ref}>
      <button className="org-trigger" onClick={() => setOpen((v) => !v)}
        aria-haspopup="listbox" aria-expanded={open}>
        <Building2 size={14} style={{ color: 'var(--amber)', flexShrink: 0 }} />
        <span className="org-name nav-label">{active.name}</span>
        <ChevronsUpDown size={12} style={{ marginLeft: 'auto', color: 'var(--faint)', flexShrink: 0 }} />
      </button>

      {open && (
        <div className="org-menu" role="listbox">
          <div className="org-menu-label">Organizations</div>
          {orgs.map((o) => (
            <button key={o.id} className="org-item" role="option" aria-selected={o.id === active.id}
              onClick={() => { setActiveOrg(o.id); setOpen(false); }}>
              <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{o.name}</span>
              {o.id === active.id && <Check size={12} style={{ color: 'var(--amber)', flexShrink: 0 }} />}
            </button>
          ))}
          <div className="org-menu-sep" />
          <Link to="/settings?tab=org" className="org-item" onClick={() => setOpen(false)}>
            <Settings2 size={12} /> Manage organization
          </Link>
          {platformAdmin && (
            <button className="org-item" onClick={() => { setOpen(false); setCreating(true); }}>
              <Plus size={12} /> New organization
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
          <Field label="Name">
            {(p) => (
              <input className="input" {...p} autoFocus value={newName} placeholder="e.g. Homelab, Acme Corp"
                onChange={(e) => setNewName(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && create()} />
            )}
          </Field>
          <button className="btn primary" style={{ width: '100%' }} onClick={create}>
            <Plus size={13} /> Create organization
          </button>
        </Modal>
      )}
    </div>
  );
}
