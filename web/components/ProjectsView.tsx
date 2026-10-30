"use client";

import Link from "next/link";
import { useState } from "react";
import { useApi } from "@/lib/api";
import { fmtDate, healthLabel } from "@/lib/format";
import type { Health, Project } from "@/lib/types";
import { Icon } from "./Icon";
import { Avatar, Avatars, Button, Card, Empty, HealthBadge, Heading, IconButton, LoadError, Loading, Notice, Progress, ProjectIcon } from "./ui";
import { useSession } from "./session";

export function ProjectsView({ workspaceId, personal }: { workspaceId: string; personal: boolean }) {
  const { refreshKey, openProjectForm } = useSession();
  const [archived, setArchived] = useState(false);
  const [query, setQuery] = useState("");
  const [health, setHealth] = useState("");
  const [layout, setLayout] = useState<"grid" | "list">("grid");
  const res = useApi<{ items: Project[]; can_create: boolean }>(`/workspaces/${workspaceId}/projects${archived ? "?state=ARCHIVED" : ""}`, refreshKey);

  if (res.error) return <LoadError message={res.error.status === 404 ? "This workspace isn’t available to you." : res.error.message} onRetry={res.reload} />;
  if (!res.data) return <Loading />;

  const canCreate = res.data.can_create;
  const create = () => openProjectForm({ workspaceId, personal });
  const ps = res.data.items.filter((p) => (!query || p.name.toLowerCase().includes(query.toLowerCase())) && (!health || p.health === health));

  return (
    <>
      <Heading
        title={personal ? "A space that’s just yours." : "Projects, moving together."}
        sub={personal ? "Personal goals and projects, kept separate from enterprise work." : "The big picture, the details, and the people bringing it to life."}
        actions={canCreate && <Button icon="plus" variant="primary" onClick={create}>New project</Button>}
      />
      {personal && <Notice tone="purple" icon="lock">Your private workspace · Only you can see these projects. They never appear in organization reports.</Notice>}
      <div className="toolbar">
        <div className="filter-group">
          <div className="filter-search">
            <Icon name="search" />
            <input placeholder="Find a project…" aria-label="Find a project" value={query} onChange={(e) => setQuery(e.target.value)} />
          </div>
          <select aria-label="Project health" value={health} onChange={(e) => setHealth(e.target.value)}>
            <option value="">All health</option>
            {(Object.keys(healthLabel) as Health[]).map((h) => <option key={h} value={h}>{healthLabel[h]}</option>)}
          </select>
        </div>
        <Button icon="folder" onClick={() => setArchived((a) => !a)}>{archived ? "Show active" : "Show archived"}</Button>
        <div className="segmented">
          <button className={layout === "grid" ? "active" : ""} onClick={() => setLayout("grid")}><Icon name="dashboard" /> Grid</button>
          <button className={layout === "list" ? "active" : ""} onClick={() => setLayout("list")}><Icon name="list" /> List</button>
        </div>
      </div>
      {!ps.length && !canCreate ? (
        <Empty title="No projects to show" desc="Projects you’re a member of, or that your role can read, will appear here." />
      ) : layout === "grid" ? (
        <div className="grid3">
          {ps.map((p) => (
            <article className="card project-card" key={p.id}>
              <div className="row between">
                <ProjectIcon project={p} />
                <div className="row">
                  <HealthBadge health={p.health} />
                  {p.access === "MANAGE" && <IconButton label={`Edit ${p.name}`} icon="more" onClick={() => openProjectForm({ workspaceId, personal, projectId: p.id })} />}
                </div>
              </div>
              <h2><Link href={`/projects/${p.id}`}>{p.name}</Link></h2>
              <p>{p.description}</p>
              <div className="project-meta"><span>Task completion</span><span>{p.metrics.rate_percent}%</span></div>
              <Progress value={p.metrics.rate_percent} color={p.color} />
              <div className="project-card-footer">
                <Avatars users={p.members} />
                <span className="row tiny muted"><Icon name="calendar" className="sm" />{fmtDate(p.due_on)}</span>
              </div>
            </article>
          ))}
          {canCreate && !archived && (
            <button className="card new-project-card" onClick={create}>
              <Icon name="plus" />
              <h3>Bring a new idea to life</h3>
              <p>Create a {personal ? "private" : "company"} project</p>
            </button>
          )}
        </div>
      ) : (
        <Card title="Project directory">
          <div className="table-wrap">
            <table>
              <thead><tr><th>Project</th><th>Owner</th><th>Health</th><th>Progress</th><th>Target</th></tr></thead>
              <tbody>
                {ps.map((p) => (
                  <tr key={p.id}>
                    <td><div className="row"><ProjectIcon project={p} /><Link className="task-title" href={`/projects/${p.id}`}>{p.name}</Link></div></td>
                    <td><Avatar user={p.owner} /></td>
                    <td><HealthBadge health={p.health} /></td>
                    <td style={{ minWidth: 120 }}><Progress value={p.metrics.rate_percent} color={p.color} /><p className="tiny muted" style={{ marginTop: 5 }}>{p.metrics.rate_percent}%</p></td>
                    <td>{fmtDate(p.due_on)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}
    </>
  );
}
