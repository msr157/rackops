import { useCallback, useEffect, useId, useState } from 'react';
import {
  AlertTriangle, Building2, CalendarDays, Check, Copy, Pencil, Plus, RefreshCw, Shield, Trash2,
  UserPlus, UsersRound,
} from 'lucide-react';
import { api, getActiveOrg, setActiveOrg } from '../api';
import type { Invitation, Organization, OrgMember, OrgPermissions, Project, User } from '../api';
import { useToast, cleanError } from './toast';
import { useConfirm } from './confirm';
import type { RowMenuItem } from './ui';
import { EmptyState, Field, InfoTip, Modal, Pill, RowMenu, SkeletonRows, Spinner, timeAgo } from './ui';

// The identity mark on member/invitation rows: a neutral initial disc.
// Inline-styled because the design system has no non-interactive avatar class
// (`.avatar-btn` is the header trigger, with hover affordances a static mark
// must not borrow) and this surface may not add CSS.
const initialDisc: React.CSSProperties = {
  width: 28, height: 28, borderRadius: '50%', flex: 'none',
  background: 'var(--bg-2)', border: '1px solid var(--border-hi)',
  display: 'grid', placeItems: 'center',
  fontSize: 11, fontWeight: 600, color: 'var(--dim)',
};

function Initial({ name }: { name: string }) {
  return <span style={initialDisc} aria-hidden>{(name.trim().charAt(0) || '?').toUpperCase()}</span>;
}

const clip: React.CSSProperties = { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' };

// OrgPanel: the Settings → Organization tab. Manage members (role + granular
// permissions) and invitation links, rename the active org, and — last, on its
// own — delete it. Plain sections on the canvas, in the register of Profile.
export function OrgPanel() {
  const toast = useToast();
  const confirm = useConfirm();
  const [org, setOrg] = useState<Organization | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  const [users, setUsers] = useState<User[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [invites, setInvites] = useState<Invitation[]>([]);
  const [me, setMe] = useState<User | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [renameBusy, setRenameBusy] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [busyMember, setBusyMember] = useState<{ id: string; op: 'role' | 'remove' } | null>(null);
  const [revoking, setRevoking] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [editing, setEditing] = useState<OrgMember | null>(null);
  const [inviting, setInviting] = useState(false);
  const renameId = useId();

  const load = useCallback(() => {
    setLoadFailed(false);
    api.me().then(setMe).catch(() => {});
    // Users list is admin-only; members simply see IDs fall back to "them".
    api.users().then((u) => setUsers(u ?? [])).catch(() => {});
    api.projects().then((p) => setProjects(p ?? [])).catch(() => {});
    return api.orgs().then((orgs) => {
      const active = (orgs ?? []).find((o) => o.id === getActiveOrg()) ?? orgs?.[0] ?? null;
      setOrg(active);
      setLoaded(true);
      if (active) {
        setName(active.name);
        api.invitations(active.id).then((i) => setInvites(i ?? [])).catch(() => setInvites([]));
      }
    }).catch(() => { setLoadFailed(true); });
  }, []);
  useEffect(() => { load(); }, [load]);

  // Switching orgs in the sidebar while this tab is open must swap the panel.
  useEffect(() => {
    const onOrg = () => { load(); };
    window.addEventListener('rackops-org-changed', onOrg);
    return () => window.removeEventListener('rackops-org-changed', onOrg);
  }, [load]);

  if (!org) {
    // Loading, unreachable, and genuinely-empty are three different truths.
    if (!loaded && !loadFailed) return <SkeletonRows count={3} height={72} />;
    if (loadFailed) {
      return (
        <EmptyState icon={<AlertTriangle size={18} />} title="Couldn’t load your organization">
          <div style={{ marginBottom: 12 }}>
            The server didn’t answer. Nothing was changed; retry once the connection is back.
          </div>
          <button className="btn" onClick={() => { load(); }}>
            <RefreshCw size={13} aria-hidden /> Retry
          </button>
        </EmptyState>
      );
    }
    return (
      <EmptyState icon={<Building2 size={18} />} title="No organization yet">
        Admins can create one from the sidebar switcher.
      </EmptyState>
    );
  }

  const userLabel = (id: string) => {
    const u = users.find((x) => x.id === id);
    return u ? (u.fullName || u.email) : id;
  };
  const userEmail = (id: string) => users.find((x) => x.id === id)?.email ?? '';
  const myMembership = org.members.find((m) => m.userId === me?.id);
  const canManage = me?.role === 'owner' || me?.role === 'admin' ||
    myMembership?.role === 'owner' || myMembership?.role === 'admin';

  const rename = async () => {
    if (renameBusy) return;
    if (!name.trim() || name.trim() === org.name) { setRenaming(false); setName(org.name); return; }
    setRenameBusy(true);
    try {
      await api.renameOrg(org.id, name.trim());
      toast.ok('Organization renamed.');
      setRenaming(false);
      await load();
    } catch (e) { toast.err(cleanError(e)); }
    setRenameBusy(false);
  };

  const removeOrg = async () => {
    if (deleting) return;
    const yes = await confirm({
      title: `Delete "${org.name}"?`,
      body: 'Only possible when the organization has no projects, machines, or domains. Members keep their accounts.',
      confirmLabel: 'Delete organization', danger: true,
    });
    if (!yes) return;
    setDeleting(true);
    try {
      await api.deleteOrg(org.id);
      toast.ok('Organization deleted.');
      setActiveOrg('');
      await load();
    } catch (e) { toast.err(cleanError(e)); }
    setDeleting(false);
  };

  // Routine member↔admin flips apply instantly; only the two moves that shift
  // control of the organization pause for a confirm. Returns false when the
  // change didn't land so the caller can snap the select back.
  const setRoleFor = async (m: OrgMember, role: string): Promise<boolean> => {
    if (busyMember || role === m.role) return false;
    if (role === 'owner') {
      const yes = await confirm({
        title: `Make ${userLabel(m.userId)} an owner?`,
        body: 'Owners hold the organization. They can rename or delete it and manage every member, including you.',
        confirmLabel: 'Make owner',
      });
      if (!yes) return false;
    } else if (m.role === 'admin') {
      const yes = await confirm({
        title: `Remove ${userLabel(m.userId)}’s admin role?`,
        body: 'They stay in the organization but can no longer manage members, invitations, or settings.',
        confirmLabel: `Make ${role}`,
      });
      if (!yes) return false;
    }
    setBusyMember({ id: m.userId, op: 'role' });
    try {
      await api.setOrgMember(org.id, m.userId, { role });
      toast.ok(`${userLabel(m.userId)} is now ${role}.`);
      await load();
      setBusyMember(null);
      return true;
    } catch (e) {
      toast.err(cleanError(e));
      setBusyMember(null);
      return false;
    }
  };

  const removeMember = async (m: OrgMember) => {
    if (busyMember) return;
    const yes = await confirm({
      title: `Remove ${userLabel(m.userId)} from ${org.name}?`,
      body: 'They lose access to this organization’s projects and machines. Their account is not deleted.',
      confirmLabel: 'Remove member', danger: true,
    });
    if (!yes) return;
    setBusyMember({ id: m.userId, op: 'remove' });
    try {
      await api.removeOrgMember(org.id, m.userId);
      toast.ok('Member removed.');
      await load();
    } catch (e) { toast.err(cleanError(e)); }
    setBusyMember(null);
  };

  const dropInvite = async (inv: Invitation) => {
    if (revoking) return;
    const yes = await confirm({
      title: `Revoke the invitation for ${inv.email}?`,
      body: 'Their link stops working immediately. You can always send a new one.',
      confirmLabel: 'Revoke invitation', danger: true,
    });
    if (!yes) return;
    setRevoking(inv.id);
    try {
      await api.deleteInvitation(org.id, inv.id);
      toast.ok('Invitation revoked.');
      await load();
    } catch (e) { toast.err(cleanError(e)); }
    setRevoking(null);
  };

  return (
    <>
      {/* Members lead: they are the day-to-day work of this tab. Identity
          follows; deletion lives alone at the bottom, past extra whitespace. */}
      <section>
        <div className="section-title" style={{ marginTop: 4 }}>
          Members
          <InfoTip wide>
            Owners hold the organization; admins manage members and settings; members act
            within the permissions you grant them, including which projects they can see.
          </InfoTip>
          {canManage && (
            <button className="btn sm primary" style={{ marginLeft: 'auto' }} onClick={() => setInviting(true)}>
              <UserPlus size={12} aria-hidden /> Invite
            </button>
          )}
        </div>
        <div className="dim" style={{ marginBottom: 4 }}>
          Everyone with access to this organization and the role each one holds.
        </div>

        <div className="list">
          {org.members.map((m) => {
            const rowBusy = busyMember?.id === m.userId;
            const label = userLabel(m.userId);
            const email = userEmail(m.userId);
            const actions: RowMenuItem[] = [];
            if (canManage && m.role === 'member') {
              actions.push({
                label: 'Edit permissions', icon: <Shield size={13} aria-hidden />,
                disabled: rowBusy, onSelect: () => setEditing(m),
              });
            }
            if (canManage && me?.id !== m.userId) {
              actions.push({
                label: 'Remove from organization', icon: <Trash2 size={13} aria-hidden />,
                danger: true, disabled: rowBusy, onSelect: () => removeMember(m),
              });
            }
            return (
              <div key={m.userId} className="list-row">
                <Initial name={label} />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div className="row" style={{ gap: 8, minWidth: 0 }}>
                    <strong style={clip}>{label}</strong>
                    {me?.id === m.userId && <span className="tag">you</span>}
                  </div>
                  {email && email !== label && <div className="dim" style={clip}>{email}</div>}
                </div>
                {canManage && me?.id !== m.userId ? (
                  <>
                    {rowBusy && busyMember?.op === 'role' && <Spinner size={12} />}
                    <select className="input" style={{ width: 110, padding: '5px 8px' }} value={m.role}
                      disabled={rowBusy}
                      aria-label={`Role for ${label}`}
                      onChange={(e) => {
                        // A controlled select doesn't repaint when the app
                        // skips the re-render (confirm cancelled, request
                        // failed) — snap the DOM value back ourselves.
                        const el = e.currentTarget;
                        setRoleFor(m, e.target.value).then((applied) => {
                          if (!applied) el.value = m.role;
                        });
                      }}>
                      <option value="owner">owner</option>
                      <option value="admin">admin</option>
                      <option value="member">member</option>
                    </select>
                  </>
                ) : (
                  <Pill status={m.role} />
                )}
                {actions.length > 0 && (
                  rowBusy && busyMember?.op === 'remove'
                    ? <Spinner size={14} />
                    : <RowMenu label={`Actions for ${label}`} items={actions} />
                )}
              </div>
            );
          })}
        </div>
      </section>

      {/* Pending invitations */}
      {canManage && invites.length > 0 && (
        <section>
          <div className="section-title">Pending invitations</div>
          <div className="dim" style={{ marginBottom: 4 }}>
            Links are shown once at creation. Revoke and re-invite if one was lost.
          </div>
          <div className="list">
            {invites.map((inv) => (
              <div key={inv.id} className="list-row">
                <Initial name={inv.email} />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={clip}><strong>{inv.email}</strong></div>
                  <div className="dim" style={clip}>
                    {inv.role} · <span title={new Date(inv.expiresAt).toLocaleString()}>{expiresIn(inv.expiresAt)}</span>
                  </div>
                </div>
                <button className="icon-sq" disabled={revoking === inv.id}
                  aria-label={`Revoke invitation for ${inv.email}`} title="Revoke invitation"
                  onClick={() => dropInvite(inv)}>
                  {revoking === inv.id ? <Spinner size={13} /> : <Trash2 size={14} />}
                </button>
              </div>
            ))}
          </div>
        </section>
      )}

      {/* Identity: org facts as kv stripes, with the rename living inline
          on the Name row behind the pencil. */}
      <section>
        <div className="section-title">Organization</div>
        <div className="kv-rows">
          <div className="kv-row">
            <span className="kv-label"><Building2 size={13} aria-hidden /> Name</span>
            {renaming ? (
              <>
                <label className="sr-only" htmlFor={renameId}>Organization name</label>
                <input className="input" id={renameId} style={{ maxWidth: 280, marginLeft: 'auto' }}
                  value={name} autoFocus disabled={renameBusy}
                  onChange={(e) => setName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') rename();
                    if (e.key === 'Escape') { setRenaming(false); setName(org.name); }
                  }} />
                <button className="btn sm primary" onClick={rename} disabled={renameBusy}>
                  {renameBusy ? <Spinner size={12} /> : <Check size={12} aria-hidden />} Save
                </button>
                <button className="btn sm ghost" disabled={renameBusy}
                  onClick={() => { setRenaming(false); setName(org.name); }}>
                  Cancel
                </button>
              </>
            ) : (
              <>
                <span className="kv-val">{org.name}</span>
                {canManage && (
                  <button className="icon-sq sm quiet" aria-label="Rename organization" title="Rename organization"
                    onClick={() => setRenaming(true)}>
                    <Pencil size={12} />
                  </button>
                )}
              </>
            )}
          </div>
          <div className="kv-row">
            <span className="kv-label"><UsersRound size={13} aria-hidden /> Members</span>
            <span className="kv-val">{org.members.length}</span>
          </div>
          <div className="kv-row">
            <span className="kv-label"><CalendarDays size={13} aria-hidden /> Created</span>
            <span className="kv-val" title={new Date(org.createdAt).toLocaleString()}>
              {timeAgo(org.createdAt)}
            </span>
          </div>
        </div>
      </section>

      {/* The one irreversible action on this tab, kept last and apart from
          the routine controls by whitespace alone. */}
      {canManage && (
        <section>
          <div className="section-title" style={{ marginTop: 44 }}>Danger zone</div>
          <div className="row spread wrap">
            <span className="dim" style={{ flex: 1, minWidth: 220 }}>
              Deleting is only possible while the organization has no projects, machines, or
              domains. Members keep their accounts.
            </span>
            <button className="btn danger" onClick={removeOrg} disabled={deleting}>
              {deleting ? <Spinner size={13} /> : <Trash2 size={13} aria-hidden />} Delete organization
            </button>
          </div>
        </section>
      )}

      {editing && (
        <PermissionsModal org={org} member={editing} projects={projects}
          label={userLabel(editing.userId)}
          onClose={() => setEditing(null)}
          onSaved={() => { setEditing(null); load(); }} />
      )}
      {inviting && (
        <InviteModal org={org} onClose={() => setInviting(false)} onDone={() => { setInviting(false); load(); }} />
      )}
    </>
  );
}

/* ---- Permission editor: Dokploy-style capability switches + project tree ---- */

function PermissionsModal({ org, member, projects, label, onClose, onSaved }: {
  org: Organization; member: OrgMember; projects: Project[]; label: string;
  onClose: () => void; onSaved: () => void;
}) {
  const toast = useToast();
  const scopeGroup = useId();
  const [perms, setPerms] = useState<OrgPermissions>({ ...member.permissions });
  // The backend stores "all projects" as an empty list. The editor never shows
  // that inversion: an explicit radio pair carries the intent, and the list of
  // ticked projects survives a round-trip through "All projects" unharmed.
  const storedSelection = member.permissions.accessedProjects ?? [];
  const [scope, setScope] = useState<'all' | 'selected'>(storedSelection.length === 0 ? 'all' : 'selected');
  const [selected, setSelected] = useState<string[]>(storedSelection);
  const [busy, setBusy] = useState(false);
  const scopeError = scope === 'selected' && selected.length === 0;

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
    if (busy || scopeError) return;
    setBusy(true);
    try {
      // The empty-array-means-all convention is mapped exactly here, nowhere
      // else: the radios above are the single source of intent.
      const permissions: OrgPermissions = {
        ...perms,
        accessedProjects: scope === 'all' ? [] : selected,
      };
      await api.setOrgMember(org.id, member.userId, { permissions });
      toast.ok(`Permissions updated for ${label}.`);
      onSaved();
    } catch (e) { toast.err(cleanError(e)); setBusy(false); }
  };

  return (
    <Modal title={`Permissions for ${label}`} icon={<Shield size={15} />} width={520} onClose={onClose}>
      <div className="stack" style={{ gap: 6, marginBottom: 16 }}>
        {SWITCHES.map((s) => (
          <label key={s.key} className="check" style={{ alignItems: 'flex-start', gap: 9 }}>
            <input type="checkbox" checked={!!perms[s.key]} disabled={busy}
              onChange={(e) => setPerms({ ...perms, [s.key]: e.target.checked })} />
            <span>
              <strong style={{ fontWeight: 600 }}>{s.label}</strong>
              <span className="faint" style={{ display: 'block' }}>{s.hint}</span>
            </span>
          </label>
        ))}
      </div>

      <div className="section-title" style={{ margin: '0 0 8px' }}>
        Project access
        <InfoTip wide>
          All projects covers projects created later, too. Only selected projects limits this
          member to the ticked projects until you change them here.
        </InfoTip>
      </div>
      <div className="stack" style={{ gap: 6, marginBottom: 8 }} role="radiogroup" aria-label="Project access">
        <label className="check">
          <input type="radio" name={scopeGroup} checked={scope === 'all'} disabled={busy}
            onChange={() => setScope('all')} />
          <span>All projects (current and future)</span>
        </label>
        <label className="check">
          <input type="radio" name={scopeGroup} checked={scope === 'selected'} disabled={busy}
            onChange={() => setScope('selected')} />
          <span>Only selected projects</span>
        </label>
      </div>
      {scope === 'selected' && (
        <div className="stack" style={{ gap: 4, marginLeft: 22, marginBottom: 8 }}>
          {projects.map((p) => {
            const on = selected.includes(p.id);
            return (
              <label key={p.id} className="check">
                <input type="checkbox" checked={on} disabled={busy}
                  onChange={(e) => {
                    const cur = new Set(selected);
                    if (e.target.checked) cur.add(p.id); else cur.delete(p.id);
                    setSelected([...cur]);
                  }} />
                <span>{p.name}</span>
              </label>
            );
          })}
          {projects.length === 0 && <span className="faint">No projects in this organization yet.</span>}
          {scopeError && <span className="field-error" role="alert">Select at least one project</span>}
        </div>
      )}

      <button className="btn primary" style={{ width: '100%', marginTop: 10 }}
        disabled={busy || scopeError} onClick={save}>
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
    if (busy) return;
    if (!email.includes('@')) { toast.err('Enter the invitee’s email address.'); return; }
    setBusy(true);
    try {
      const r = await api.createInvitation(org.id, email.trim(), role);
      setLink(r.link);
    } catch (e) { toast.err(cleanError(e)); }
    setBusy(false);
  };

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true); setTimeout(() => setCopied(false), 1400);
    } catch {
      toast.err('Couldn’t reach the clipboard. Select the link and copy it manually.');
    }
  };

  return (
    <Modal title={`Invite to ${org.name}`} icon={<UserPlus size={15} />} width={460}
      onClose={link ? onDone : onClose}>
      {!link ? (
        <>
          <div className="dim" style={{ marginBottom: 14 }}>
            You&rsquo;ll get a link to send them. It lets them create their account already
            inside this organization. Links expire after 48 hours.
          </div>
          <Field label="Email">
            {(p) => (
              <input className="input" {...p} type="email" value={email} placeholder="teammate@example.com" autoFocus
                onChange={(e) => setEmail(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && create()} />
            )}
          </Field>
          <Field label="Role"
            hint={role === 'admin'
              ? 'Admins manage members, invitations, and settings.'
              : 'Members work within the permissions you grant them.'}>
            {(p) => (
              <select className="input" {...p} value={role} onChange={(e) => setRole(e.target.value)}>
                <option value="member">member</option>
                <option value="admin">admin</option>
              </select>
            )}
          </Field>
          <button className="btn primary" style={{ width: '100%' }} disabled={busy} onClick={create}>
            {busy ? <Spinner size={13} /> : <Plus size={13} />} Create invitation link
          </button>
        </>
      ) : (
        <>
          <div className="notice warn" style={{ marginBottom: 12 }}>
            <AlertTriangle size={14} aria-hidden />
            <span>Copy this link now. It&rsquo;s shown only once.</span>
          </div>
          <div className="row" style={{ gap: 8, marginBottom: 14 }}>
            <code className="code-block" style={{ flex: 1, wordBreak: 'break-all' }}>{link}</code>
            <button className="icon-sq" onClick={copy}
              aria-label="Copy invitation link" title="Copy invitation link">
              {copied ? <Check size={14} style={{ color: 'var(--ok)' }} /> : <Copy size={14} />}
            </button>
          </div>
          <button className="btn primary" style={{ width: '100%' }} onClick={onDone}>Done</button>
        </>
      )}
    </Modal>
  );
}

// Future-tense sibling of ui.tsx's timeAgo: invitations expire, they don't age.
function expiresIn(iso: string): string {
  const s = (new Date(iso).getTime() - Date.now()) / 1000;
  if (s <= 0) return 'expired';
  if (s < 3600) return `expires in ${Math.max(1, Math.round(s / 60))}m`;
  if (s < 86400) return `expires in ${Math.round(s / 3600)}h`;
  return `expires in ${Math.round(s / 86400)}d`;
}
