"use client";

import { useParams, useRouter } from "next/navigation";
import { useApi } from "@/lib/api";
import type { List, Project } from "@/lib/types";
import { Board, type Lane } from "@/components/Board";
import { Icon } from "@/components/Icon";
import { Button, Heading, LoadError, Loading } from "@/components/ui";
import { useSession } from "@/components/session";

export default function ProjectBoard() {
  const { projectId } = useParams<{ projectId: string }>();
  const router = useRouter();
  const { me, activeOrg, refreshKey, openTaskForm } = useSession();
  const board = useApi<{ project: Project; lanes: Lane[] }>(`/projects/${projectId}/board`, refreshKey);
  const orgProjects = useApi<List<Project>>(activeOrg ? `/workspaces/${activeOrg.workspace_id}/projects` : null);
  const personalProjects = useApi<List<Project>>(me.personal_workspace_id ? `/workspaces/${me.personal_workspace_id}/projects` : null);

  if (board.error) return <LoadError message={board.error.status === 404 ? "This board doesn’t exist or isn’t shared with you." : board.error.message} />;
  if (!board.data) return <Loading />;

  const p = board.data.project;
  const canCreate = (p.access === "MANAGE" || p.access === "CONTRIBUTE") && p.state === "ACTIVE";
  const choices = [...(orgProjects.data?.items ?? []), ...(personalProjects.data?.items ?? [])];
  if (!choices.some((c) => c.id === p.id)) choices.unshift(p);
  const count = board.data.lanes.reduce((n, l) => n + l.tasks.length, 0);

  return (
    <>
      <Heading
        title="A clearer flow of work."
        sub="See the next step. Surface dependencies. Keep your team in sync."
        actions={canCreate && <Button icon="plus" variant="primary" onClick={() => openTaskForm({ defaults: { projectId: p.id } })}>Create task</Button>}
      />
      <div className="toolbar">
        <div className="filter-group">
          <select aria-label="Board project" value={p.id} onChange={(e) => router.push(`/projects/${e.target.value}/board`)}>
            {choices.map((c) => (
              <option key={c.id} value={c.id}>{c.name}{c.workspace_kind === "PERSONAL" ? " · Private" : ""}</option>
            ))}
          </select>
          <span className="pill"><Icon name="users" className="sm" /> {count} tasks</span>
        </div>
        <span className="tiny muted">Drag to move · The server enforces allowed status transitions</span>
      </div>
      <Board lanes={board.data.lanes} projectId={p.id} canCreate={canCreate} />
      <p className="tiny muted" style={{ marginTop: 12 }}>WIP guideline: 4 tasks per lane · Cancelled tasks are available in My Work and the task register.</p>
    </>
  );
}
