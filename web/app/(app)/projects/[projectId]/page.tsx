"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useState, type FormEvent } from "react";
import { api, errorMessage, useApi } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import type { List, Milestone, Project, Task, User } from "@/lib/types";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Avatar, Avatars, Button, Card, HealthBadge, IconButton, LoadError, Loading, Metric, Progress, ProjectIcon, Tabs } from "@/components/ui";
import { TaskTable } from "@/components/TaskTable";
import { useSession } from "@/components/session";

type Tab = "overview" | "tasks";

export default function ProjectDetail() {
  const { projectId } = useParams<{ projectId: string }>();
  const { me, refreshKey, refresh, toast, openTaskForm, openProjectForm } = useSession();
  const [tab, setTab] = useState<Tab>("overview");
  const [accessOpen, setAccessOpen] = useState(false);
  const [ms, setMs] = useState({ title: "", target_on: "" });
  const project = useApi<Project & { milestones: Milestone[] }>(`/projects/${projectId}`, refreshKey);
  const tasks = useApi<List<Task>>(`/projects/${projectId}/tasks`, refreshKey);

  if (project.error) return <LoadError message={project.error.status === 404 ? "This project doesn’t exist or isn’t shared with you." : project.error.message} />;
  if (!project.data || !tasks.data) return <Loading />;

  const p = project.data;
  const m = p.metrics;
  const ts = tasks.data.items;
  const personal = p.workspace_kind === "PERSONAL";
  const back = personal ? "/personal/projects" : `/workspaces/${p.workspace_id}/projects`;
  const canAdd = p.access === "MANAGE" || p.access === "CONTRIBUTE";
  const manage = p.access === "MANAGE";

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

  async function addMilestone(e: FormEvent) {
    e.preventDefault();
    if (await act(() => api(`/projects/${p.id}/milestones`, { method: "POST", body: ms }), `Added ${ms.title}`)) setMs({ title: "", target_on: "" });
  }

  return (
    <>
      <div style={{ marginBottom: 17 }}>
        <Link className="textbtn" href={back}><Icon name="left" className="sm" /> Back to {personal ? "personal projects" : "projects"}</Link>
      </div>
      <div className="page-heading">
        <div className="project-heading">
          <ProjectIcon project={p} />
          <div>
            <h1>{p.name}</h1>
            <p>{p.department} · {personal ? "Private workspace" : me.organizations.find((o) => o.id === p.tenant_id)?.name} · Due {fmtDate(p.due_on)}{p.state === "ARCHIVED" ? " · Archived" : ""}</p>
          </div>
        </div>
        <div className="row actions">
          <Link className="btn" href={`/projects/${p.id}/board`}><Icon name="columns" className="sm" />Board</Link>
          {p.access === "MANAGE" && <Button icon="pen" onClick={() => openProjectForm({ workspaceId: p.workspace_id, personal, projectId: p.id })}>Edit project</Button>}
          {canAdd && p.state === "ACTIVE" && <Button icon="plus" variant="primary" onClick={() => openTaskForm({ defaults: { projectId: p.id } })}>Add task</Button>}
        </div>
      </div>
      <div className="metrics">
        <Metric label="Eligible tasks" value={m.eligible} sub="Cancelled work excluded" />
        <Metric label="Completion" value={`${m.rate_percent}%`} sub={`${m.done} completed tasks`} icon="target" tone="green" />
        <Metric label="Needs attention" value={m.attention} sub="Unique blocked + overdue tasks" icon="warning" tone="orange" />
        <Metric label="Remaining effort" value={`${m.open_hours}h`} sub="Estimates for all open tasks" icon="clock" tone="blue" />
      </div>
      <Tabs<Tab> items={[["overview", "Overview"], ["tasks", "Tasks", ts.length]]} active={tab} onChange={setTab} />
      {tab === "overview" ? (
        <div className="dashboard-grid">
          <div>
            <Card title="A shared direction">
              <p>{p.description}</p>
              <div className="grid2" style={{ marginTop: 20 }}>
                <div>
                  <div className="tiny muted">ACCOUNTABLE OWNER</div>
                  <div className="row" style={{ marginTop: 10 }}><Avatar user={p.owner} /><h3>{p.owner?.display_name}</h3></div>
                </div>
                <div>
                  <div className="tiny muted">PROJECT TEAM</div>
                  <div className="row" style={{ marginTop: 10 }}>
                    <Avatars users={p.members} />
                    {!personal && <button className="textbtn" onClick={() => setAccessOpen(true)}>{manage ? "Manage access" : "View access"}</button>}
                  </div>
                </div>
              </div>
            </Card>
            <Card title="Milestones that matter">
              {p.milestones.map((m) => (
                <div className="milestone" key={m.id}>
                  <input type="checkbox" checked={m.state === "DONE"} disabled={!manage} aria-label={`${m.title} complete`}
                    onChange={(e) => act(() => api(`/milestones/${m.id}`, { method: "PATCH", body: { state: e.target.checked ? "DONE" : "OPEN" } }), e.target.checked ? `Completed ${m.title}` : `Reopened ${m.title}`)} />
                  <span className={`milestone-title ${m.state === "DONE" ? "done-title" : ""}`}>{m.title}</span>
                  <span className="spacer" />
                  <span className="tiny muted">{fmtDate(m.target_on)}</span>
                  {manage && <IconButton label={`Delete ${m.title}`} icon="trash" onClick={() => confirm(`Delete milestone “${m.title}”?`) && act(() => api(`/milestones/${m.id}`, { method: "DELETE" }), `Deleted ${m.title}`)} />}
                </div>
              ))}
              {!p.milestones.length && <p className="tiny muted">No milestones yet.</p>}
              {manage && (
                <form className="row" style={{ gap: 8, marginTop: 12 }} onSubmit={addMilestone}>
                  <input aria-label="Milestone title" placeholder="New milestone" value={ms.title} onChange={(e) => setMs((x) => ({ ...x, title: e.target.value }))} required maxLength={100} style={{ flex: 1 }} />
                  <input aria-label="Target date" type="date" value={ms.target_on} onChange={(e) => setMs((x) => ({ ...x, target_on: e.target.value }))} required />
                  <button type="submit" className="btn">Add</button>
                </form>
              )}
            </Card>
          </div>
          <div>
            <Card title="Project health">
              <HealthBadge health={p.health} />
              <p style={{ marginTop: 15 }}>
                {m.blocked ? `${m.blocked} blocked tasks require a dependency review.` : m.overdue ? `${m.overdue} overdue tasks require a timeline review.` : "No blocked or overdue tasks in this project."}
              </p>
              <div style={{ margin: "20px 0 8px" }}><Progress value={m.rate_percent} color={p.color} /></div>
              <p className="tiny">{m.done} of {m.eligible} eligible tasks complete · {m.rate_percent}%</p>
              <p className="tiny muted" style={{ marginTop: 10 }}>Your access: {p.access.toLowerCase()} · as of {me.today}</p>
            </Card>
            <Card title="Upcoming work">
              <TaskTable tasks={ts.filter((t) => t.status !== "DONE" && t.status !== "CANCELLED").slice(0, 4)} compact />
            </Card>
          </div>
        </div>
      ) : (
        <Card title="Project task register">
          <TaskTable tasks={ts} />
        </Card>
      )}
      {accessOpen && p.tenant_id && <AccessModal projectId={p.id} orgId={p.tenant_id} ownerId={p.owner?.id} act={act} onClose={() => setAccessOpen(false)} />}
    </>
  );
}

type Access = { user: User; access_level: string };
const levelLabel: Record<string, string> = { MANAGER: "Manager", CONTRIBUTOR: "Contributor", VIEWER: "Viewer" };

function AccessModal({ projectId, orgId, ownerId, act, onClose }: {
  projectId: string; orgId: string; ownerId?: string; act: (fn: () => Promise<unknown>, done: string) => Promise<boolean>; onClose: () => void;
}) {
  const { me, refreshKey } = useSession();
  const list = useApi<List<Access> & { can_manage: boolean }>(`/projects/${projectId}/access`, refreshKey);
  const members = useApi<List<User & { state: string }>>(list.data?.can_manage ? `/organizations/${orgId}/members` : null, refreshKey);
  const [add, setAdd] = useState({ user_id: "", access_level: "CONTRIBUTOR" });
  const set = (uid: string, level: string, name: string) =>
    act(() => api(`/projects/${projectId}/access/${uid}`, { method: "PUT", body: { access_level: level } }), `${name} is now a ${levelLabel[level].toLowerCase()}`);

  const taken = new Set([ownerId, ...(list.data?.items.map((a) => a.user.id) ?? [])]);
  const candidates = (members.data?.items ?? []).filter((m) => m.state === "ACTIVE" && !taken.has(m.id));
  const canManage = list.data?.can_manage ?? false;

  return (
    <Modal title="Project access" onClose={onClose}>
      {list.error ? <LoadError message={list.error.message} onRetry={list.reload} /> : !list.data ? <Loading /> : (
        <>
          {list.data.items.map((a) => (
            <div className="settings-row" key={a.user.id}>
              <div className="row"><Avatar user={a.user} size="sm" /><h3>{a.user.display_name}{a.user.id === me.user.id ? " (you)" : ""}</h3></div>
              {canManage && a.user.id !== me.user.id ? (
                <div className="row">
                  <select aria-label={`Access for ${a.user.display_name}`} value={a.access_level} style={{ fontSize: 10 }} onChange={(e) => set(a.user.id, e.target.value, a.user.display_name)}>
                    {Object.entries(levelLabel).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
                  </select>
                  <IconButton label={`Remove ${a.user.display_name}`} icon="trash" onClick={() => act(() => api(`/projects/${projectId}/access/${a.user.id}`, { method: "DELETE" }), `Removed ${a.user.display_name}`)} />
                </div>
              ) : (
                <span className="tiny muted">{levelLabel[a.access_level]}</span>
              )}
            </div>
          ))}
          {!list.data.items.length && <p className="tiny muted">Only the project owner has access.</p>}
          {canManage && (
            <form className="row" style={{ gap: 8, marginTop: 16 }} onSubmit={(e) => {
              e.preventDefault();
              const name = candidates.find((c) => c.id === add.user_id)?.display_name ?? "Member";
              set(add.user_id, add.access_level, name).then((ok) => ok && setAdd({ user_id: "", access_level: "CONTRIBUTOR" }));
            }}>
              <select aria-label="Person" value={add.user_id} onChange={(e) => setAdd((x) => ({ ...x, user_id: e.target.value }))} required style={{ flex: 1 }}>
                <option value="">Add an organization member…</option>
                {candidates.map((c) => <option key={c.id} value={c.id}>{c.display_name}</option>)}
              </select>
              <select aria-label="Access level" value={add.access_level} onChange={(e) => setAdd((x) => ({ ...x, access_level: e.target.value }))}>
                {Object.entries(levelLabel).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
              </select>
              <button type="submit" className="btn primary">Add</button>
            </form>
          )}
          <p className="tiny muted" style={{ marginTop: 14 }}>The project owner always has full access. Organization admins can manage every project.</p>
        </>
      )}
    </Modal>
  );
}
