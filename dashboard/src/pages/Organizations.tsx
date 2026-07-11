import { useCallback, useEffect, useState } from 'react';
import { Building2, Plus, Trash2, UserPlus, Pencil, Check, X } from 'lucide-react';
import { api } from '../api';
import type { Organization, User, Project } from '../api';
import { useToast, cleanError } from '../components/toast';
import { useConfirm } from '../components/confirm';
import { InfoTip } from '../components/ui';

// OrganizationsPage: create and manage organisations (Dokploy-style).
// Projects belong to an organisation; members join with a per-org role.
export function OrganizationsPage() {
  const toast = useToast();
  const confirm = useConfirm();

  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [newOrg, setNewOrg] = useState('');
  const [renaming, setRenaming] = useState('');   // org id being renamed
  const [renameVal, setRenameVal] = useState('');
  const [invite, setInvite] = useState<Record<string, { email: string; role: string }>>({});

  const load = useCallback(() => {
    api.orgs().then((o) => setOrgs(o ?? [])).catch(() => {});
    api.users().then((u) => setUsers(u ?? [])).catch(() => {});
    api.projects().then((p) => setProjects(p ?? [])).catch(() => {});
  }, []);
  useEffect(load, [load]);

  const userLabel = (id: string) => {
    const u = users.find((x) => x.id === id);
    return u ? (u.fullName || u.email) : id;
  };

  const create = async () => {
    if (!newOrg.trim()) return;
    try {
      await api.createOrg(newOrg.trim());
      toast.ok(`Organisation "${newOrg.trim()}" created.`);
      setNewOrg('');
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  const rename = async (o: Organization) => {
    if (!renameVal.trim() || renameVal.trim() === o.name) { setRenaming(''); return; }
    try {
      await api.renameOrg(o.id, renameVal.trim());
      toast.ok('Renamed.');
      setRenaming('');
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  const remove = async (o: Organization) => {
    const projCount = projects.filter((p) => p.orgId === o.id).length;
    const yes = await confirm({
      title: `Delete organisation "${o.name}"?`,
      body: projCount > 0
        ? `It still contains ${projCount} project${projCount > 1 ? 's' : ''} — deletion will be refused until they're moved or removed.`
        : 'Members lose access to it. This can’t be undone.',
      confirmLabel: 'Delete', danger: true,
    });
    if (!yes) return;
    try {
      await api.deleteOrg(o.id);
      toast.ok(`"${o.name}" deleted.`);
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  const addMember = async (o: Organization) => {
    const inv = invite[o.id] ?? { email: '', role: 'member' };
    if (!inv.email.trim()) { toast.err('Enter the email of an existing account.'); return; }
    try {
      await api.addOrgMember(o.id, inv.email.trim(), inv.role);
      toast.ok(`${inv.email.trim()} added to ${o.name}.`);
      setInvite((prev) => ({ ...prev, [o.id]: { email: '', role: 'member' } }));
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  const removeMember = async (o: Organization, userId: string) => {
    const yes = await confirm({
      title: `Remove ${userLabel(userId)} from ${o.name}?`,
      body: 'They keep their account but lose this organisation’s projects.',
      confirmLabel: 'Remove', danger: true,
    });
    if (!yes) return;
    try {
      await api.removeOrgMember(o.id, userId);
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  return (
    <div className="page" style={{ maxWidth: 800 }}>
      <div className="page-title">Organizations</div>
      <div className="page-sub">
        Group projects and people — each project lives in exactly one organisation
      </div>

      <div className="card">
        <div className="row" style={{ marginBottom: 0 }}>
          <input className="input" placeholder="New organisation name (e.g. Homelab, Acme Corp)"
            value={newOrg} onChange={(e) => setNewOrg(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && create()} />
          <button className="btn primary" onClick={create}><Plus size={13} /> Create</button>
        </div>
      </div>

      {orgs.map((o) => {
        const members = o.members ?? [];
        const orgProjects = projects.filter((p) => p.orgId === o.id);
        const inv = invite[o.id] ?? { email: '', role: 'member' };
        return (
          <div className="card" key={o.id}>
            <div className="row spread" style={{ marginBottom: 12 }}>
              <div className="row" style={{ gap: 10 }}>
                <Building2 size={16} style={{ color: 'var(--amber)' }} />
                {renaming === o.id ? (
                  <span className="row" style={{ gap: 6 }}>
                    <input className="input" style={{ width: 220 }} value={renameVal} autoFocus
                      onChange={(e) => setRenameVal(e.target.value)}
                      onKeyDown={(e) => { if (e.key === 'Enter') rename(o); if (e.key === 'Escape') setRenaming(''); }} />
                    <button className="btn sm" onClick={() => rename(o)} aria-label="Save name"><Check size={12} /></button>
                    <button className="btn sm ghost" onClick={() => setRenaming('')} aria-label="Cancel"><X size={12} /></button>
                  </span>
                ) : (
                  <strong style={{ fontSize: 15 }}>{o.name}</strong>
                )}
                <span className="faint">
                  {orgProjects.length} project{orgProjects.length === 1 ? '' : 's'} · {members.length} member{members.length === 1 ? '' : 's'}
                </span>
              </div>
              <div className="row">
                {renaming !== o.id && (
                  <button className="btn sm" onClick={() => { setRenaming(o.id); setRenameVal(o.name); }}>
                    <Pencil size={12} /> Rename
                  </button>
                )}
                <button className="btn sm danger" onClick={() => remove(o)} aria-label={`Delete ${o.name}`}>
                  <Trash2 size={12} />
                </button>
              </div>
            </div>

            {orgProjects.length > 0 && (
              <div className="dim" style={{ marginBottom: 12 }}>
                Projects: {orgProjects.map((p) => p.name).join(', ')}
              </div>
            )}

            <div className="eyebrow" style={{ marginBottom: 8 }}>
              Members
              <InfoTip text="owner: full control of the organisation · admin: manage members and projects · member: work inside its projects. Accounts are created in Settings; here you add them to the organisation." wide />
            </div>
            {members.map((m) => (
              <div className="row spread" key={m.userId} style={{ padding: '7px 0', borderTop: '1px solid var(--border)' }}>
                <span>
                  {userLabel(m.userId)}{' '}
                  <span className={`badge ${m.role === 'owner' ? 'amber' : m.role === 'admin' ? 'purple' : 'gray'}`}>{m.role}</span>
                </span>
                <button className="btn sm danger" aria-label={`Remove ${userLabel(m.userId)}`}
                  onClick={() => removeMember(o, m.userId)}>
                  <Trash2 size={12} />
                </button>
              </div>
            ))}
            {members.length === 0 && <div className="faint" style={{ padding: '6px 0' }}>No members yet.</div>}

            <div className="row" style={{ marginTop: 12 }}>
              <input className="input" placeholder="member@email — must be an existing account"
                value={inv.email}
                onChange={(e) => setInvite((prev) => ({ ...prev, [o.id]: { ...inv, email: e.target.value } }))}
                onKeyDown={(e) => e.key === 'Enter' && addMember(o)} />
              <select className="input" style={{ width: 130 }} value={inv.role}
                onChange={(e) => setInvite((prev) => ({ ...prev, [o.id]: { ...inv, role: e.target.value } }))}>
                <option value="member">member</option>
                <option value="admin">admin</option>
                <option value="owner">owner</option>
              </select>
              <button className="btn" onClick={() => addMember(o)}><UserPlus size={13} /> Add</button>
            </div>
          </div>
        );
      })}
    </div>
  );
}
