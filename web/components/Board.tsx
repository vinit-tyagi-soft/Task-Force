"use client";

import { useState } from "react";
import { api, errorMessage } from "@/lib/api";
import { fmtDate, statusLabel } from "@/lib/format";
import type { Status, Task } from "@/lib/types";
import { Icon } from "./Icon";
import { Avatar, IconButton, PriorityTag } from "./ui";
import { useSession } from "./session";

export interface Lane {
  status: Status;
  wip_limit: number;
  over_limit: boolean;
  tasks: Task[];
}

const laneColors = ["#b6b3c8", "#8fa8d3", "#b6a2d8", "#d5a2aa", "#8fb9a7"];

export function Board({ lanes, projectId, canCreate }: { lanes: Lane[]; projectId: string; canCreate: boolean }) {
  const { openTask, openTaskForm, refresh, toast } = useSession();
  const [over, setOver] = useState<Status | null>(null);
  const byId = new Map(lanes.flatMap((l) => l.tasks).map((t) => [t.id, t]));

  async function move(taskId: string, status: Status) {
    const t = byId.get(taskId);
    if (!t || t.status === status) return;
    try {
      await api(`/tasks/${t.id}`, { method: "PATCH", version: t.version, body: { status } });
      toast(`Moved ${t.title} to ${statusLabel[status]}`);
    } catch (e) {
      toast(errorMessage(e), "warning");
    }
    refresh();
  }

  return (
    <div className="board-wrap">
      <div className="kanban">
        {lanes.map((lane, i) => (
          <section
            key={lane.status}
            className={`lane ${over === lane.status ? "drag-over" : ""}`}
            style={{ ["--lane" as string]: laneColors[i] }}
            onDragOver={(e) => {
              e.preventDefault();
              setOver(lane.status);
            }}
            onDragLeave={() => setOver(null)}
            onDrop={(e) => {
              e.preventDefault();
              setOver(null);
              move(e.dataTransfer.getData("text/task-id"), lane.status);
            }}
          >
            <div className="lane-head">
              <span className="lane-dot" />
              {statusLabel[lane.status]}
              <span className="count">{lane.tasks.length}</span>
              <span className="spacer" />
              {canCreate && <IconButton label={`Add task in ${statusLabel[lane.status]}`} icon="plus" onClick={() => openTaskForm({ defaults: { projectId, status: lane.status } })} />}
            </div>
            {lane.over_limit && <p className="wip">WIP guideline of {lane.wip_limit} exceeded</p>}
            {lane.tasks.map((t) => (
              <article
                key={t.id}
                className="kanban-card"
                draggable={t.can_edit}
                onDragStart={(e) => e.dataTransfer.setData("text/task-id", t.id)}
              >
                <div className="row between">
                  <PriorityTag priority={t.priority} />
                  <IconButton label={`Open ${t.title}`} icon="more" onClick={() => openTask(t.id)} />
                </div>
                <button className="task-title" onClick={() => openTask(t.id)}>{t.title}</button>
                <p>{t.project.name}</p>
                <div className="kanban-bottom">
                  <span className={`row tiny ${t.overdue ? "overdue" : "muted"}`}><Icon name="calendar" className="sm" />{fmtDate(t.due_on)}</span>
                  <span className="row">
                    {t.comment_count > 0 && <span className="row tiny muted"><Icon name="comment" className="sm" />{t.comment_count}</span>}
                    <Avatar user={t.owner} size="sm" />
                  </span>
                </div>
              </article>
            ))}
            {canCreate && <button className="textbtn" onClick={() => openTaskForm({ defaults: { projectId, status: lane.status } })}>+ Add task</button>}
          </section>
        ))}
      </div>
    </div>
  );
}
