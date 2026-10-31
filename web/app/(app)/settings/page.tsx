"use client";

import { useState, type FormEvent } from "react";
import { api, errorMessage, useApi } from "@/lib/api";
import { roleLabel } from "@/lib/format";
import type { AuditEvent, List, User } from "@/lib/types";
import { Avatar, Badge, Card, Empty, Heading, LoadError, Loading, Notice } from "@/components/ui";
import { useSession } from "@/components/session";

type Member = User & { state: string; roles: string[] };
interface Invitation { org_id: string; name: string; invited_at: string }

export default function Settings() {
  const { me, activeOrg, refreshKey, refresh, toast, can } = useSession();
  const canAudit = can("audit.read");
  const admin = can("org.members.invite");
  const owner = activeOrg?.roles.includes("TENANT_OWNER") ?? false;
  const base = activeOrg ? `/organizations/${activeOrg.id}` : null;
  const audit = useApi<List<AuditEvent>>(base && canAudit ? `${base}/audit-events?limit=100` : null, refreshKey);
  const members = useApi<List<Member>>(base && admin ? `${base}/members` : null, refreshKey);
  const invitations = useApi<List<Invitation>>("/me/invitations", refreshKey);

  async function act(fn: () => Promise<unknown>, done: string) {
    try {
      await fn();
      toast(done);
      refresh();
      return true;
    } catch (err) {
      toast(errorMessage(err), "warning");
      return false;
    }
  }

  return (
    <>
      <Heading title="Settings & administration" sub="Your profile, organizations, people and access, and the audit trail." />
      <div className="dashboard-grid">
        <div>
          {activeOrg && admin && (
            <>
              <OrgSettings key={`${activeOrg.id}-${activeOrg.name}-${activeOrg.reporting_timezone}`} act={act} />
              {can("ai.manage_policy") && <AIPolicyCard orgId={activeOrg.id} act={act} />}
              <Card title="People & access" sub={`${activeOrg.name} · roles apply on the next request`}>
                <InviteForm act={act} owner={owner} />
                {members.error ? (
                  <LoadError message={members.error.message} onRetry={members.reload} />
                ) : !members.data ? (
                  <Loading />
                ) : (
                  members.data.items.map((m) => {
                    const self = m.id === me.user.id;
                    const locked = !owner && m.roles.includes("TENANT_OWNER");
                    return (
                      <div className="settings-row" key={m.id}>
                        <div className="row">
                          <Avatar user={m} size="sm" />
                          <div>
                            <h3>{m.display_name}{self ? " (you)" : ""}</h3>
                            <p>{m.roles.map((r) => roleLabel[r] ?? r).join(", ") || "No role"}</p>
                          </div>
                          {m.state !== "ACTIVE" && <Badge label={m.state.toLowerCase()} tone={m.state === "INVITED" ? "blue" : "orange"} />}
                        </div>
                        <div className="row">
                          <select aria-label={`Role for ${m.display_name}`} value={m.roles[0] ?? ""} disabled={locked} style={{ fontSize: 10 }}
                            onChange={(e) => act(() => api(`${base}/members/${m.id}/roles`, { method: "PUT", body: { roles: [e.target.value] } }), `${m.display_name} is now ${roleLabel[e.target.value]}`)}>
                            {Object.entries(roleLabel).filter(([k]) => owner || k !== "TENANT_OWNER" || m.roles.includes(k)).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
                          </select>
                          {!self && !locked && m.state === "ACTIVE" && (
                            <button className="textbtn" onClick={() => act(() => api(`${base}/members/${m.id}`, { method: "PATCH", body: { state: "SUSPENDED" } }), `Suspended ${m.display_name}`)}>Suspend</button>
                          )}
                          {!self && !locked && m.state === "SUSPENDED" && (
                            <button className="textbtn" onClick={() => act(() => api(`${base}/members/${m.id}`, { method: "PATCH", body: { state: "ACTIVE" } }), `Reactivated ${m.display_name}`)}>Reactivate</button>
                          )}
                          {!self && !locked && (
                            <button className="textbtn" style={{ color: "var(--red)" }}
                              onClick={() => confirm(`Remove ${m.display_name} from ${activeOrg.name}? Their access ends immediately.`) && act(() => api(`${base}/members/${m.id}`, { method: "PATCH", body: { state: "REMOVED" } }), `Removed ${m.display_name}`)}>
                              {m.state === "INVITED" ? "Revoke" : "Remove"}
                            </button>
                          )}
                        </div>
                      </div>
                    );
                  })
                )}
              </Card>
            </>
          )}
          <Card title="Audit trail" sub={activeOrg ? `${activeOrg.name} · append-only, newest first` : undefined}>
            {!activeOrg ? (
              <Empty title="No organization selected" desc="Audit events are scoped to an organization." />
            ) : !canAudit ? (
              <Notice>Your role in {activeOrg.name} does not include audit review. Tenant owners, org admins and auditors can see this trail.</Notice>
            ) : audit.error ? (
              <LoadError message={audit.error.message} onRetry={audit.reload} />
            ) : !audit.data ? (
              <Loading />
            ) : audit.data.items.length === 0 ? (
              <Empty title="No events yet" desc="Changes to projects and tasks will appear here." />
            ) : (
              <div className="table-wrap">
                <table>
                  <thead><tr><th>When</th><th>Actor</th><th>Action</th><th>Summary</th><th>Request</th></tr></thead>
                  <tbody>
                    {audit.data.items.map((e) => (
                      <tr key={e.id}>
                        <td className="tiny">{new Date(e.occurred_at).toLocaleString("en-GB", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}</td>
                        <td><div className="row"><Avatar user={e.actor} size="sm" /><span className="tiny">{e.actor?.display_name}</span></div></td>
                        <td><code className="tiny">{e.action}</code></td>
                        <td className="tiny">{e.summary}</td>
                        <td className="tiny muted" title={e.request_id}>{e.request_id.slice(0, 8)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </div>
        <div>
          <Card title="Your profile">
            <div className="row" style={{ marginBottom: 14 }}>
              <Avatar user={me.user} size="lg" />
              <div>
                <h3>{me.user.display_name}</h3>
                <p>{me.user.title}</p>
                <p className="tiny muted">{me.user.email} · {me.user.timezone}</p>
              </div>
            </div>
          </Card>
          {!!invitations.data?.items.length && (
            <Card title="Invitations">
              {invitations.data.items.map((i) => (
                <div className="settings-row" key={i.org_id}>
                  <div><h3>{i.name}</h3><p>Invited {new Date(i.invited_at).toLocaleDateString("en-GB")}</p></div>
                  <button className="btn primary" onClick={() => act(() => api(`/organizations/${i.org_id}/invitations/accept`, { method: "POST" }), `Joined ${i.name}`)}>Accept</button>
                </div>
              ))}
            </Card>
          )}
          <Card title="Memberships">
            {me.organizations.map((o) => (
              <div className="settings-row" key={o.id}>
                <h3>{o.name}</h3>
                <span className="tiny muted">{o.roles.map((r) => roleLabel[r] ?? r).join(", ")}</span>
              </div>
            ))}
            <p className="tiny muted" style={{ marginTop: 12 }}>Roles are granted by organization administrators and enforced by the server on every request.</p>
            <CreateOrg act={act} />
          </Card>
        </div>
      </div>
    </>
  );
}

type Act = (fn: () => Promise<unknown>, done: string) => Promise<boolean>;

function OrgSettings({ act }: { act: Act }) {
  const { activeOrg, can } = useSession();
  const [v, setV] = useState({ name: activeOrg!.name, reporting_timezone: activeOrg!.reporting_timezone });
  if (!can("org.policy.manage")) return null;

  function submit(e: FormEvent) {
    e.preventDefault();
    act(() => api(`/organizations/${activeOrg!.id}`, { method: "PATCH", body: v }), "Organization settings saved");
  }

  return (
    <Card title="Organization settings">
      <form className="form-grid" onSubmit={submit}>
        <label className="field">Name<input value={v.name} onChange={(e) => setV((x) => ({ ...x, name: e.target.value }))} required maxLength={120} /></label>
        <label className="field">Reporting timezone<input value={v.reporting_timezone} onChange={(e) => setV((x) => ({ ...x, reporting_timezone: e.target.value }))} required placeholder="Asia/Kolkata" /></label>
        <div className="field full"><button type="submit" className="btn primary" style={{ justifySelf: "start" }}>Save settings</button></div>
      </form>
      <p className="tiny muted" style={{ marginTop: 10 }}>The reporting timezone defines “today” for overdue counts, reports and alerts.</p>
    </Card>
  );
}

function InviteForm({ act, owner }: { act: Act; owner: boolean }) {
  const { activeOrg } = useSession();
  const [v, setV] = useState({ email: "", display_name: "", role: "MEMBER" });

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (await act(() => api(`/organizations/${activeOrg!.id}/invitations`, { method: "POST", body: v }), `Invited ${v.email}`)) setV({ email: "", display_name: "", role: "MEMBER" });
  }

  return (
    <form className="row wrap" style={{ gap: 8, marginBottom: 16 }} onSubmit={submit}>
      <input aria-label="Name" placeholder="Full name" value={v.display_name} onChange={(e) => setV((x) => ({ ...x, display_name: e.target.value }))} required maxLength={120} />
      <input aria-label="Email" type="email" placeholder="name@company.com" value={v.email} onChange={(e) => setV((x) => ({ ...x, email: e.target.value }))} required maxLength={320} />
      <select aria-label="Role" value={v.role} onChange={(e) => setV((x) => ({ ...x, role: e.target.value }))}>
        {Object.entries(roleLabel).filter(([k]) => owner || k !== "TENANT_OWNER").map(([k, l]) => <option key={k} value={k}>{l}</option>)}
      </select>
      <button type="submit" className="btn primary">Invite</button>
    </form>
  );
}

function CreateOrg({ act }: { act: Act }) {
  const [open, setOpen] = useState(false);
  const [v, setV] = useState({ name: "", slug: "", reporting_timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC" });
  const [error, setError] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    try {
      const res = await api<{ id: string }>("/organizations", { method: "POST", body: v });
      localStorage.setItem("tf-active-org", res.id);
      await act(async () => undefined, `Created ${v.name}`);
      setOpen(false);
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  if (!open) return <button className="textbtn" style={{ marginTop: 12 }} onClick={() => setOpen(true)}>Create an organization</button>;
  return (
    <form className="form-grid" style={{ marginTop: 14 }} onSubmit={submit}>
      <label className="field full">Organization name
        <input value={v.name} required maxLength={120}
          onChange={(e) => setV((x) => ({ ...x, name: e.target.value, slug: e.target.value.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 63) }))} />
      </label>
      <label className="field">URL name<input value={v.slug} onChange={(e) => setV((x) => ({ ...x, slug: e.target.value }))} required pattern="[a-z0-9][a-z0-9\-]{1,62}" /></label>
      <label className="field">Reporting timezone<input value={v.reporting_timezone} onChange={(e) => setV((x) => ({ ...x, reporting_timezone: e.target.value }))} required /></label>
      <div className="field-error full" role="alert">{error}</div>
      <div className="row full">
        <button type="button" className="btn" onClick={() => setOpen(false)}>Cancel</button>
        <button type="submit" className="btn primary">Create organization</button>
      </div>
    </form>
  );
}

interface Policy { enabled: boolean; allowed_intents: string[]; retention_days: number; version: number }
const intentLabel: Record<string, string> = {
  BLOCKED: "Blocked work", OVERDUE: "Overdue work", PRIORITIES: "Personal priorities", WORKLOAD: "Team workload", SUMMARY: "Portfolio summary",
};

function AIPolicyCard({ orgId, act }: { orgId: string; act: Act }) {
  const { refreshKey } = useSession();
  const data = useApi<{ policy: Policy; intents: string[] }>(`/organizations/${orgId}/ai/policy`, refreshKey);
  if (data.error) return <Card title="AI policy"><LoadError message={data.error.message} onRetry={data.reload} /></Card>;
  if (!data.data) return null;
  const p = data.data.policy;
  const save = (next: Partial<Policy>, done: string) =>
    act(() => api(`/organizations/${orgId}/ai/policy`, { method: "PUT", version: p.version, body: { ...p, ...next } }), done);

  return (
    <Card title="AI policy" sub="Rules-based assistant · no external model · answers only from work each person can already see">
      <label className="settings-row">
        <span><b style={{ fontSize: 12 }}>AI Intelligence Center</b><p>{p.enabled ? "On for owners and executives" : "Off for everyone in this organization"}</p></span>
        <button type="button" className={`toggle ${p.enabled ? "on" : ""}`} role="switch" aria-checked={p.enabled} aria-label="AI Intelligence Center"
          onClick={() => save({ enabled: !p.enabled }, p.enabled ? "AI turned off" : "AI turned on")}><span /></button>
      </label>
      {data.data.intents.map((i) => (
        <label className="checklist-item" key={i}>
          <input type="checkbox" checked={p.allowed_intents.includes(i)} disabled={!p.enabled}
            onChange={(e) => save({ allowed_intents: e.target.checked ? [...p.allowed_intents, i] : p.allowed_intents.filter((x) => x !== i) }, "AI policy saved")} />
          {intentLabel[i] ?? i}
        </label>
      ))}
      <label className="settings-row">
        <span><b style={{ fontSize: 12 }}>Question history</b><p>Each person’s questions are private and deleted after this period.</p></span>
        <select aria-label="History retention" value={p.retention_days} onChange={(e) => save({ retention_days: Number(e.target.value) }, "AI policy saved")}>
          {[7, 30, 90, 365].map((d) => <option key={d} value={d}>{d} days</option>)}
        </select>
      </label>
    </Card>
  );
}
