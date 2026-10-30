"use client";

import { useEffect, useState, type FormEvent } from "react";
import { api, errorMessage, useApi } from "@/lib/api";
import { fmtDate, roleLabel } from "@/lib/format";
import type { List, User } from "@/lib/types";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Avatar, Avatars, Badge, Button, Card, Empty, Heading, IconButton, LoadError, Loading, Progress, Tabs } from "@/components/ui";
import { useSession } from "@/components/session";

interface Team {
  id: string;
  name: string;
  description: string;
  department: { id: string; name: string } | null;
  lead: User | null;
  color: string;
  members: User[];
  version: number;
  can_manage: boolean;
}
type Member = User & { state: string; roles: string[] };
interface WorkloadRow {
  user: User;
  teams: string[];
  assigned_tasks: number;
  planned_hours: number;
  capacity_hours: number;
  utilization_percent: number;
  overdue_tasks: number;
  status: "AVAILABLE" | "BALANCED" | "OVER_CAPACITY";
}
type Workload = List<WorkloadRow> & { from: string; to: string; days: number; visibility: "organization" | "team" | "self"; definition: string };
type Tab = "teams" | "members" | "workload";

const workloadBadge: Record<WorkloadRow["status"], [string, string]> = {
  OVER_CAPACITY: ["Over capacity", "orange"],
  BALANCED: ["Well balanced", "blue"],
  AVAILABLE: ["Available", "green"],
};

export default function TeamsPage() {
  const { activeOrg, refreshKey } = useSession();
  const [tab, setTab] = useState<Tab>("teams");
  const [form, setForm] = useState<{ team?: Team } | null>(null);
  const base = activeOrg ? `/organizations/${activeOrg.id}` : null;
  const teams = useApi<List<Team> & { can_manage: boolean }>(base && `${base}/teams`, refreshKey);
  const members = useApi<List<Member>>(base && `${base}/members`, refreshKey);
  const workload = useApi<Workload>(base && tab === "workload" ? `${base}/workload` : null, refreshKey);

  if (!activeOrg) return <Empty title="No organization selected" desc="Teams belong to an organization. Switch to one from the workspace menu." />;
  if (teams.error) return <LoadError message={teams.error.message} onRetry={teams.reload} />;
  if (!teams.data || !members.data) return <Loading />;
  const activeMembers = members.data.items.filter((m) => m.state === "ACTIVE");

  let body;
  if (tab === "teams")
    body = teams.data.items.length ? (
      <div className="grid3">
        {teams.data.items.map((team) => (
          <Card
            key={team.id}
            className="team-card"
            title={
              <div className="row">
                <span className="team-symbol" style={{ color: team.color, background: `${team.color}16` }}><Icon name="users" /></span>
                <div>{team.name}<p>{team.department?.name ?? "No department"}</p></div>
              </div>
            }
            extra={team.can_manage && <IconButton label={`Edit ${team.name}`} icon="more" onClick={() => setForm({ team })} />}
          >
            {team.description && <p>{team.description}</p>}
            <p>{team.lead ? `Led by ${team.lead.display_name}` : "No lead yet"} · {team.members.length} member{team.members.length === 1 ? "" : "s"}</p>
            <div className="row between">
              <Avatars users={team.members} />
              {team.can_manage && <button className="textbtn" onClick={() => setForm({ team })}>Manage team</button>}
            </div>
          </Card>
        ))}
      </div>
    ) : (
      <Empty title="No teams yet" desc="Group people who deliver together to see their shared workload." />
    );
  else if (tab === "members")
    body = (
      <div className="member-grid">
        {activeMembers.map((u) => (
          <Card key={u.id} className="member-card">
            <Avatar user={u} size="lg" />
            <h3>{u.display_name}</h3>
            <p>{u.title || "Team member"}</p>
            <p>{teams.data!.items.filter((t) => t.members.some((m) => m.id === u.id)).map((t) => t.name).join(", ") || "No team"}</p>
            <div style={{ marginTop: 13 }}>{u.roles.map((r) => <Badge key={r} label={roleLabel[r] ?? r} tone="gray" />)}</div>
          </Card>
        ))}
      </div>
    );
  else if (workload.error) body = <LoadError message={workload.error.message} onRetry={workload.reload} />;
  else if (!workload.data) body = <Loading />;
  else
    body = (
      <Card
        title={`${fmtDate(workload.data.from)} – ${fmtDate(workload.data.to)} · estimated effort`}
        sub={workload.data.visibility === "organization" ? "Everyone in the organization" : workload.data.visibility === "team" ? "You and the teams you lead" : "Only your own workload is visible to your role"}
      >
        <div className="table-wrap">
          <table className="capacity-table">
            <thead>
              <tr><th>Team member</th><th>Assigned tasks</th><th>Planned effort</th><th>Capacity</th><th>Status</th></tr>
            </thead>
            <tbody>
              {workload.data.items.map((r) => {
                const [label, tone] = workloadBadge[r.status];
                return (
                  <tr key={r.user.id} className={r.status === "OVER_CAPACITY" ? "overcapacity" : ""}>
                    <td>
                      <div className="row">
                        <Avatar user={r.user} />
                        <div><b>{r.user.display_name}</b><p className="tiny muted">{r.teams.join(", ") || r.user.title}</p></div>
                      </div>
                    </td>
                    <td>{r.assigned_tasks} tasks{r.overdue_tasks > 0 && <p className="tiny muted">+{r.overdue_tasks} overdue</p>}</td>
                    <td>{r.planned_hours}h / {r.capacity_hours}h</td>
                    <td className="capacity-bar">
                      <Progress value={Math.min(100, r.utilization_percent)} color={r.status === "OVER_CAPACITY" ? "var(--orange)" : "#a491d4"} />
                      <div className="tiny muted" style={{ marginTop: 5 }}>{r.utilization_percent}% utilization</div>
                    </td>
                    <td><Badge label={label} tone={tone} /></td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        <p className="tiny muted" style={{ marginTop: 12 }}>{workload.data.definition}</p>
      </Card>
    );

  return (
    <>
      <Heading
        title="Great work starts with great teams."
        sub="A little more visibility. A better balance for everyone."
        actions={teams.data.can_manage && <Button icon="plus" variant="primary" onClick={() => setForm({})}>Create team</Button>}
      />
      <Tabs<Tab>
        items={[["teams", "Team directory", teams.data.items.length], ["members", "People", activeMembers.length], ["workload", "Workload & capacity"]]}
        active={tab}
        onChange={setTab}
      />
      {body}
      {form && <TeamForm orgId={activeOrg.id} team={form.team} members={activeMembers} onClose={() => setForm(null)} />}
    </>
  );
}

function TeamForm({ orgId, team, members, onClose }: { orgId: string; team?: Team; members: Member[]; onClose: () => void }) {
  const { refresh, toast } = useSession();
  const [departments, setDepartments] = useState<{ id: string; name: string }[]>([]);
  const [v, setV] = useState({
    name: team?.name ?? "",
    description: team?.description ?? "",
    department_id: team?.department?.id ?? "",
    lead_user_id: team?.lead?.id ?? "",
    color: team?.color ?? "#7964da",
  });
  const [picked, setPicked] = useState<Set<string>>(new Set(team?.members.map((m) => m.id)));
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    api<List<{ id: string; name: string }>>(`/organizations/${orgId}/departments`).then((l) => setDepartments(l.items));
  }, [orgId]);

  const set = (k: keyof typeof v) => (e: { target: { value: string } }) => setV((x) => ({ ...x, [k]: e.target.value }));
  const toggle = (id: string) =>
    setPicked((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });

  async function run(fn: () => Promise<unknown>, done: string) {
    setSaving(true);
    setError("");
    try {
      await fn();
      toast(done);
      refresh();
      onClose();
    } catch (err) {
      setError(errorMessage(err));
      setSaving(false);
    }
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    const body = {
      name: v.name,
      description: v.description,
      color: v.color,
      member_ids: [...picked],
      ...(v.department_id ? { department_id: v.department_id } : {}),
      ...(v.lead_user_id ? { lead_user_id: v.lead_user_id } : {}),
    };
    run(
      () => (team ? api(`/teams/${team.id}`, { method: "PUT", version: team.version, body }) : api(`/organizations/${orgId}/teams`, { method: "POST", body })),
      team ? `Updated ${v.name}` : `Created ${v.name}`,
    );
  }

  return (
    <Modal title={team ? "Shape this team" : "Bring people together."} onClose={onClose}>
      <form onSubmit={submit}>
        <div className="form-grid">
          <label className="field full">
            Team name
            <input value={v.name} onChange={set("name")} required maxLength={80} />
          </label>
          <label className="field full">
            What does this team own?
            <textarea value={v.description} onChange={set("description")} maxLength={500} />
          </label>
          <label className="field">
            Department
            <select value={v.department_id} onChange={set("department_id")}>
              <option value="">No department</option>
              {departments.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
            </select>
          </label>
          <label className="field">
            Team lead
            <select value={v.lead_user_id} onChange={set("lead_user_id")}>
              <option value="">No lead</option>
              {members.map((m) => <option key={m.id} value={m.id}>{m.display_name}</option>)}
            </select>
          </label>
          <label className="field">
            Accent color
            <input type="color" value={v.color} onChange={set("color")} />
          </label>
          <fieldset className="field full" style={{ border: 0, padding: 0, margin: 0 }}>
            <legend>Members</legend>
            <div style={{ maxHeight: 180, overflow: "auto", display: "grid", gridTemplateColumns: "1fr 1fr", gap: 6 }}>
              {members.map((m) => (
                <label key={m.id} className="row" style={{ fontWeight: 400 }}>
                  <input type="checkbox" checked={picked.has(m.id) || m.id === v.lead_user_id} disabled={m.id === v.lead_user_id} onChange={() => toggle(m.id)} />
                  {m.display_name}
                </label>
              ))}
            </div>
          </fieldset>
        </div>
        <div className="field-error" role="alert">{error}</div>
        <div className="modal-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={saving}>{team ? "Save team" : "Create team"}</button>
        </div>
      </form>
      {team && (
        <div style={{ marginTop: 10 }}>
          <button
            className="textbtn"
            disabled={saving}
            onClick={() => confirm(`Delete ${team.name}? Members keep their work; only the grouping is removed.`) && run(() => api(`/teams/${team.id}`, { method: "DELETE", version: team.version }), `Deleted ${team.name}`)}
          >
            Delete team
          </button>
        </div>
      )}
    </Modal>
  );
}
