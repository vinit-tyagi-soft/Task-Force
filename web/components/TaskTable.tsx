"use client";

import Link from "next/link";
import { api, errorMessage } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import type { Task } from "@/lib/types";
import { Icon } from "./Icon";
import { Avatar, Button, Empty, PriorityTag, StatusBadge } from "./ui";
import { useSession } from "./session";

export function TaskTable({ tasks, compact }: { tasks: Task[]; compact?: boolean }) {
  const { openTask, openTaskForm, refresh, toast } = useSession();

  async function toggleDone(t: Task) {
    const status = t.status === "DONE" ? "TODO" : "DONE";
    try {
      await api(`/tasks/${t.id}`, { method: "PATCH", version: t.version, body: { status } });
      toast(status === "DONE" ? `Completed ${t.title}` : `Reopened ${t.title}`);
      refresh();
    } catch (e) {
      toast(errorMessage(e), "warning");
      refresh();
    }
  }

  if (!tasks.length) {
    return <Empty title="A little breathing room" desc="No tasks match your current view." action={<Button icon="plus" variant="soft" onClick={() => openTaskForm()}>Create a task</Button>} />;
  }
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Task name</th>
            {!compact && <th>Project</th>}
            <th>Status</th>
            <th>Priority</th>
            <th>Due date</th>
            <th>Owner</th>
          </tr>
        </thead>
        <tbody>
          {tasks.map((t) => (
            <tr key={t.id}>
              <td>
                <div className="task-cell">
                  <button
                    className={`task-check ${t.status === "DONE" ? "done" : ""}`}
                    aria-label={`${t.status === "DONE" ? "Reopen" : "Complete"} ${t.title}`}
                    disabled={!t.can_edit || t.status === "BLOCKED" || t.status === "CANCELLED"}
                    onClick={() => toggleDone(t)}
                  >
                    {t.status === "DONE" && <Icon name="check" />}
                  </button>
                  <div>
                    <button className={`task-title ${t.status === "DONE" ? "done-title" : ""}`} onClick={() => openTask(t.id)}>{t.title}</button>
                    <div className="task-sub">
                      {compact ? t.project.name : t.key}
                      {t.workspace_kind === "PERSONAL" && (
                        <> <span className="private-tag"><Icon name="lock" /> Private</span></>
                      )}
                    </div>
                  </div>
                </div>
              </td>
              {!compact && (
                <td>
                  <Link className="textbtn muted tiny" href={`/projects/${t.project.id}`}>{t.project.name}</Link>
                </td>
              )}
              <td><StatusBadge status={t.status} /></td>
              <td><PriorityTag priority={t.priority} /></td>
              <td className={t.overdue ? "overdue" : ""}>{fmtDate(t.due_on)}{t.overdue ? " · Late" : ""}</td>
              <td><Avatar user={t.owner} size="sm" /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
