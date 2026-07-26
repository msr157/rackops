import { useCallback, useEffect, useId, useState } from 'react';
import {
  Building2, Check, Copy, Link2, Pencil, Plus, Shield, Trash2, UserPlus, UsersRound, X,
} from 'lucide-react';
import { api, getActiveOrg, setActiveOrg } from '../api';
import type { Invitation, Organization, OrgMember, OrgPermissions, Project, User } from '../api';
import { useToast, cleanError } from './toast';
import { useConfirm } from './confirm';
import { Field, InfoTip, Modal, Spinner, timeAgo } from './ui';

// OrgPanel: the Settings → Organization tab. Rename/delete the active org,
// manage members (role + granular permissions), and mint invitation links.
export function OrgPanel() {
  const toast = useToast();
  const confirm = useConfirm();
  const [org, setOrg] = useState<Organization | null>(null);
  const [users, setUsers] = useState<User[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [invites, setInvites] = useState<Invitation[]>([]);
  const [me, setMe] = useState<User | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState('');
  const [editing, setEditing] = useState<OrgMember | null>(null);
  const [inviting, setInviting] = useState(false);
  const renameId = useId();

  const load = useCallback(() => {
    api.orgs().then((orgs) => {
      const active = (orgs ?? []).find((o) => o.id === getActiveOrg()) ?? orgs?.[0] ?? null;
      setOrg(active);
      if (active) {
        setName(active.name);
        api.invitations(active.id).then((i) => setInvites(i ?? [])).catch(() => setInvites([]));
      }
    }).catch(() => {});
    api.me().then(setMe).catch(() => {});
    // Users list is admin-only; members simply see IDs fall back to "them".
    api.users().then((u) => setUsers(u ?? [])).catch(() => {});
    api.projects().then((p) => setProjects(p ?? [])).catch(() => {});
  }, []);
  useEffect(load, [load]);

  if (!org) return <div className="row dim"><Spinner /> loading organization…</div>;

  const userLabel = (id: string) => {
    const u = users.find((x) => x.id === id);
    return u ? (u.fullName || u.email) : id;
  };
  const userEmail = (id: string) => users.find((x) => x.id === id)?.email ?? '';
  const myMembership = org.members.find((m) => m.userId === me?.id);
  const canManage = me?.role === 'owner' || me?.role === 'admin' ||
    myMembership?.role === 'owner' || myMembership?.role === 'admin';

  const rename = async () => {
    if (!name.trim() || name.trim() === org.name) { setRenaming(false); return; }
    try {
      await api.renameOrg(org.id, name.trim());
      toast.ok('Organization renamed.');
      setRenaming(false);
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  const removeOrg = async () => {
    const yes = await confirm({
      title: `Delete "${org.name}"?`,
      body: 'Only possible when the organization has no projects, machines, or domains. Members keep their accounts.',
      confirmLabel: 'Delete organization', danger: true,
    });
    if (!yes) return;
    try {
      await api.deleteOrg(org.id);
      toast.ok('Organization deleted.');
      setActiveOrg('');
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  const setRole = async (m: OrgMember, role: string) => {
    try {
      await api.setOrgMember(org.id, m.userId, { role });
      toast.ok(`${userLabel(m.userId)} is now ${role}.`);
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  const removeMember = async (m: OrgMember) => {
    const yes = await confirm({
      title: `Remove ${userLabel(m.userId)} from ${org.name}?`,
      body: 'They lose access to this organization’s projects and machines. Their account is not deleted.',
      confirmLabel: 'Remove member', danger: true,
    });
    if (!yes) return;
    try {
      await api.removeOrgMember(org.id, m.userId);
      toast.ok('Member removed.');
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  const dropInvite = async (inv: Invitation) => {
    try {
      await api.deleteInvitation(org.id, inv.id);
      load();
    } catch (e) { toast.err(cleanError(e)); }
  };

  return (
    <div className="stack">
      {/* Identity + danger zone */}
      <div className="card">
        <div className="eyebrow" style={{ marginBottom: 12 }}><Building2 size={12} /> Organization</div>
        <div className="row spread wrap">
          {renaming ? (
            <div className="row" style={{ flex: 1 }}>
              <label className="sr-only" htmlFor={renameId}>Organization name</label>
              <input className="input" id={renameId} style={{ maxWidth: 280 }} value={name} autoFocus
                onChange={(e) => setName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') rename();
                  if (e.key === 'Escape') { setRenaming(false); setName(org.name); }
                }} />
              <button className="btn sm primary" onClick={rename} aria-label="Save name"><Check size={12} /></button>
              <button className="btn sm ghost" aria-label="Cancel rename"
                onClick={() => { setRenaming(false); setName(org.name); }}><X size={12} /></button>
            </div>
          ) : (
            <div className="row">
              <strong style={{ fontFamily: 'var(--display)', fontSize: 15 }}>{org.name}</strong>
              <span className="faint">created {timeAgo(org.createdAt)}</span>
              {canManage && (
                <button className="btn sm ghost" aria-label="Rename organization" onClick={() => setRenaming(true)}>
                  <Pencil size={12} />
                </button>
              )}
            </div>
          )}
          {canManage && (
            <button className="btn sm danger" onClick={removeOrg}><Trash2 size={12} /> Delete</button>
          )}
        </div>
      </div>

      {/* Members */}
      <div className="card">
        <div className="row spread" style={{ marginBottom: 12 }}>
          <div className="eyebrow" style={{ margin: 0 }}>
            <UsersRound size={12} /> Members
            <InfoTip wide>
              Owners hold the organization; admins manage members and settings; members act
              within the permissions you grant them below — including which projects they can see.
            </InfoTip>
          </div>
          {canManage && (
            <button className="btn sm primary" onClick={() => setInviting(true)}>
              <UserPlus size={12} /> Invite
            </button>
          )}
        </div>

        {org.members.map((m) => (
          <div key={m.userId} className="row spread wrap" style={{ borderTop: '1px solid var(--border)', padding: '9px 0' }}>
            <div className="row" style={{ minWidth: 0 }}>
              <strong>{userLabel(m.userId)}</strong>
              {me?.id === m.userId && <span className="tag">you</span>}
              {userEmail(m.userId) && <span className="faint">{userEmail(m.userId)}</span>}
            </div>
            <div className="row">
              {canManage && me?.id !== m.userId ? (
                <select className="input" style={{ width: 110, padding: '5px 8px' }} value={m.role}
                  aria-label={`Role for ${userLabel(m.userId)}`}
                  onChange={(e) => setRole(m, e.target.value)}>
                  <option value="owner">owner</option>
                  <option value="admin">admin</option>
                  <option value="member">member</option>
                </select>
              ) : (
                <span className="badge">{m.role}</span>
              )}
              {canManage && m.role === 'member' && (
                <button className="btn sm" aria-label={`Edit permissions for ${userLabel(m.userId)}`} onClick={() => setEditing(m)}>
                  <Shield size={12} /> Permissions
                </button>
              )}
              {canManage && me?.id !== m.userId && (
                <button className="btn sm danger" aria-label={`Remove ${userLabel(m.userId)}`} onClick={() => removeMember(m)}>
                  <Trash2 size={12} />
                </button>
              )}
            </div>
          </div>
        ))}
      </div>

      {/* Pending invitations */}
      {canManage && invites.length > 0 && (
        <div className="card">
          <div className="eyebrow" style={{ marginBottom: 10 }}><Link2 size={12} /> Pending invitations</div>
          {invites.map((inv) => (
            <div key={inv.id} className="row spread" style={{ borderTop: '1px solid var(--border)', padding: '8px 0' }}>
              <div className="row">
                <span>{inv.email}</span>
                <span className="tag">{inv.role}</span>
                <span className="faint">expires {new Date(inv.expiresAt).toLocaleString()}</span>
              </div>
              <button className="btn sm danger" aria-label="Revoke invitation" onClick={() => dropInvite(inv)}>
                <Trash2 size={12} />
              </button>
            </div>
          ))}
          <div className="faint" style={{ marginTop: 8 }}>
            Links are shown once at creation — revoke and re-invite if one was lost.
          </div>
        </div>
      )}

      {editing && (
        <PermissionsDrawer org={org} member={editing} projects={projects}
          label={userLabel(editing.userId)}
          onClose={() => setEditing(null)}
          onSaved={() => { setEditing(null); load(); }} />
      )}
      {inviting && (
        <InviteModal org={org} onClose={() => setInviting(false)} onDone={() => { setInviting(false); load(); }} />
      )}
    </div>
  );
}

/* ---- Permission editor: Dokploy-style capability switches + project tree ---- */

function PermissionsDrawer({ org, member, projects, label, onClose, onSaved }: {
  org: Organization; member: OrgMember; projects: Project[]; label: string;
  onClose: () => void; onSaved: () => void;
}) {
  const toast = useToast();
  const [perms, setPerms] = useState<OrgPermissions>({ ...member.permissions });
  const [busy, setBusy] = useState(false);
  const allProjects = perms.accessedProjects === undefined || perms.accessedProjects.length === 0;

  const SWITCHES: { key: keyof OrgPermissions; label: string; hint: string }[] = [
    { key: 'createProjects', label: 'Create projects', hint: 'Start new projects in this organization' },
    { key: 'deleteProjects', label: 'Delete projects', hint: 'Remove whole projects and their apps' },
    { key: 'createServices', label: 'Deploy apps', hint: 'Launch new apps and databases' },
    { key: 'deleteServices', label: 'Remove apps', hint: 'Tear down running apps' },
    { key: 'manageDomains', label: 'Manage domains', hint: 'Add/remove domains and public routes' },
    { key: 'manageMachines', label: 'Manage machines', hint: 'Add machines to the fleet (join command)' },
    { key: 'accessApi', label: 'API access', hint: 'Create API keys for CLI/automation' },
  ];

  const save = async () => {
    setBusy(true);
    try {
      await api.setOrgMember(org.id, member.userId, { permissions: perms });
      toast.ok(`Permissions updated for ${label}.`);
      onSaved();
    } catch (e) { toast.err(cleanError(e)); setBusy(false); }
  };

  return (
    <Modal title={`Permissions — ${label}`} icon={<Shield size={15} />} width={520} onClose={onClose}>
      <div className="stack" style={{ gap: 6, marginBottom: 16 }}>
        {SWITCHES.map((s) => (
          <label key={s.key} className="check" style={{ alignItems: 'flex-start', gap: 9 }}>
            <input type="checkbox" checked={!!perms[s.key]}
              onChange={(e) => setPerms({ ...perms, [s.key]: e.target.checked })} />
            <span>
              <strong style={{ fontWeight: 600 }}>{s.label}</strong>
              <span className="faint" style={{ display: 'block' }}>{s.hint}</span>
            </span>
          </label>
        ))}
      </div>

      <div className="eyebrow" style={{ marginBottom: 8 }}>Project access</div>
      <label className="check" style={{ marginBottom: 6 }}>
        <input type="checkbox" checked={allProjects}
          onChange={(e) => setPerms({ ...perms, accessedProjects: e.target.checked ? [] : projects.map((p) => p.id) })} />
        <span>All projects (current and future)</span>
      </label>
      {!allProjects && (
        <div className="stack" style={{ gap: 4, marginLeft: 22, marginBottom: 8 }}>
          {projects.map((p) => {
            const on = (perms.accessedProjects ?? []).includes(p.id);
            return (
              <label key={p.id} className="check">
                <input type="checkbox" checked={on}
                  onChange={(e) => {
                    const cur = new Set(perms.accessedProjects ?? []);
                    if (e.target.checked) cur.add(p.id); else cur.delete(p.id);
                    setPerms({ ...perms, accessedProjects: [...cur] });
                  }} />
                <span>{p.name}</span>
              </label>
            );
          })}
          {projects.length === 0 && <span className="faint">no projects yet</span>}
        </div>
      )}

      <button className="btn primary" style={{ width: '100%', marginTop: 10 }} disabled={busy} onClick={save}>
        {busy ? <Spinner size={13} /> : <Check size={13} />} Save permissions
      </button>
    </Modal>
  );
}

/* ---- Invite: email + role -> copy-link ---- */

function InviteModal({ org, onClose, onDone }: { org: Organization; onClose: () => void; onDone: () => void }) {
  const toast = useToast();
  const [email, setEmail] = useState('');
  const [role, setRole] = useState('member');
  const [link, setLink] = useState('');
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  const create = async () => {
    if (!email.includes('@')) { toast.err('Enter the invitee’s email address.'); return; }
    setBusy(true);
    try {
      const r = await api.createInvitation(org.id, email.trim(), role);
      setLink(r.link);
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  return (
    <Modal title={`Invite to ${org.name}`} icon={<UserPlus size={15} />} width={460}
      onClose={link ? onDone : onClose}>
      {!link ? (
        <>
          <div className="dim" style={{ marginBottom: 14 }}>
            You&rsquo;ll get a link to send them — it lets them create their account already
            inside this organization. Links expire after 48 hours.
          </div>
          <Field label="Email">
            {(p) => (
              <input className="input" {...p} type="email" value={email} placeholder="teammate@example.com" autoFocus
                onChange={(e) => setEmail(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && create()} />
            )}
          </Field>
          <Field label="Role">
            {(p) => (
              <select className="input" {...p} value={role} onChange={(e) => setRole(e.target.value)}>
                <option value="member">member — works within granted permissions</option>
                <option value="admin">admin — manages members and settings</option>
              </select>
            )}
          </Field>
          <button className="btn primary" style={{ width: '100%' }} disabled={busy} onClick={create}>
            {busy ? <Spinner size={13} /> : <Plus size={13} />} Create invitation link
          </button>
        </>
      ) : (
        <>
          <div className="badge amber" style={{ width: '100%', marginBottom: 12 }}>
            Copy this link now — it&rsquo;s shown only once.
          </div>
          <div className="row" style={{ gap: 8, marginBottom: 14 }}>
            <code className="code-block" style={{ flex: 1, wordBreak: 'break-all' }}>{link}</code>
            <button className="btn sm" onClick={() => {
              navigator.clipboard.writeText(link);
              setCopied(true); setTimeout(() => setCopied(false), 1400);
            }}>
              {copied ? <Check size={12} style={{ color: 'var(--ok)' }} /> : <Copy size={12} />}
            </button>
          </div>
          <button className="btn primary" style={{ width: '100%' }} onClick={onDone}>Done</button>
        </>
      )}
    </Modal>
  );
}
