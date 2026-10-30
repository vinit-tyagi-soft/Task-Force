"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { useApi } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import type { List, Milestone, Project, Task } from "@/lib/types";
import { Icon } from "@/components/Icon";
import { Avatar, Avatars, Button, Card, HealthBadge, LoadError, Loading, Metric, Progress, ProjectIcon, Tabs } from "@/components/ui";
import { TaskTable } from "@/components/TaskTable";
import { useSession } from "@/components/session";

type Tab = "overview" | "tasks";

export default function ProjectDetail() {
  const { projectId } = useParams<{ projectId: string }>();
  const { me, refreshKey, openTaskForm, openProjectForm } = useSession();
  const [tab, setTab] = useState<Tab>("overview");
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
                  <div style={{ marginTop: 10 }}><Avatars users={p.members} /></div>
                </div>
              </div>
            </Card>
            <Card title="Milestones that matter">
              {p.milestones.map((ms) => (
                <div className="milestone" key={ms.id}>
                  <input type="checkbox" checked={ms.state === "DONE"} disabled aria-label={ms.title} />
                  <span className={`milestone-title ${ms.state === "DONE" ? "done-title" : ""}`}>{ms.title}</span>
                  <span className="spacer" />
                  <span className="tiny muted">{fmtDate(ms.target_on)}</span>
                </div>
              ))}
              {!p.milestones.length && <p className="tiny muted">No milestones yet.</p>}
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
    </>
  );
}
