"use client";

import Link from "next/link";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, errorMessage, useApi } from "@/lib/api";
import { fmtDate, STATUSES, statusLabel } from "@/lib/format";
import type { Comment, List, Status, Task } from "@/lib/types";
import { Icon } from "./Icon";
import { Avatar, Badge, Button, IconButton, PriorityTag } from "./ui";
import { useSession } from "./session";
import { TaskExtras } from "./TaskExtras";

export function TaskDrawer({ taskId, onClose }: { taskId: string; onClose: () => void }) {
  const { activeOrg, refreshKey, refresh, toast, openTaskForm } = useSession();
  const task = useApi<Task>(`/tasks/${taskId}`, refreshKey);
  const comments = useApi<List<Comment>>(`/tasks/${taskId}/comments`, refreshKey);
  const [comment, setComment] = useState("");
  const [busy, setBusy] = useState(false);
  const ref = useRef<HTMLElement>(null);
  const close = useRef(onClose);
  useEffect(() => {
    close.current = onClose;
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close.current();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  useEffect(() => {
    if (task.data) ref.current?.querySelector<HTMLElement>("button")?.focus();
  }, [task.data]);

  async function setStatus(t: Task, status: Status) {
    try {
      await api(`/tasks/${t.id}`, { method: "PATCH", version: t.version, body: { status } });
      toast(`Moved ${t.title} to ${statusLabel[status]}`);
      refresh();
    } catch (e) {
      toast(errorMessage(e), "warning");
      task.reload();
    }
  }

  async function post(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await api(`/tasks/${taskId}/comments`, { method: "POST", body: { body: comment } });
      setComment("");
      comments.reload();
      refresh();
    } catch (err) {
      toast(errorMessage(err), "warning");
    } finally {
      setBusy(false);
    }
  }

  const t = task.data;
  return (
    <div id="drawer-root">
      <div className="drawer-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
        <section ref={ref} className="drawer" role="dialog" aria-modal="true" aria-label="Task details">
          {!t ? (
            <div className="row between">
              <p>{task.error ? errorMessage(task.error) : "Loading…"}</p>
              <IconButton label="Close task details" icon="close" onClick={onClose} />
            </div>
          ) : (
            <>
              <div className="row between">
                <span className="row tiny muted">
                  <Icon name="checkSquare" className="sm" /> {t.key} <span>/</span> {t.project.name}
                </span>
                <div className="row">
                  {t.can_edit && <IconButton label="Edit task" icon="pen" onClick={() => openTaskForm({ taskId: t.id })} />}
                  <IconButton label="Close task details" icon="close" onClick={onClose} />
                </div>
              </div>
              <h1>{t.title}</h1>
              <div className="row">
                <Badge label={t.workspace_kind === "PERSONAL" ? "Private personal work" : activeOrg?.name ?? "Organization"} tone="gray" />
                <PriorityTag priority={t.priority} />
              </div>
              <div className="drawer-section">
                <div className="detail-props">
                  <label htmlFor="drawer-status">Status</label>
                  <select id="drawer-status" value={t.status} disabled={!t.can_edit} onChange={(e) => setStatus(t, e.target.value as Status)}>
                    {STATUSES.map((s) => (
                      <option key={s} value={s}>{statusLabel[s]}</option>
                    ))}
                  </select>
                  <label>Owner</label>
                  <span className="row"><Avatar user={t.owner} />{t.owner?.display_name}</span>
                  <label>Due date</label>
                  <span className={t.overdue ? "overdue" : ""}>{fmtDate(t.due_on, { year: "numeric" })}{t.overdue ? " · Late" : ""}</span>
                  <label>Estimate</label>
                  <span>{t.estimate_hours} hours</span>
                  <label>Project</label>
                  <Link className="textbtn" href={`/projects/${t.project.id}`} onClick={onClose}>
                    {t.project.name} <Icon name="arrowUp" className="sm" />
                  </Link>
                </div>
              </div>
              <div className="drawer-section">
                <h3>A little context</h3>
                <p style={{ whiteSpace: "pre-line" }}>{t.description || "No description added."}</p>
                {t.blocked_reason && (
                  <div className="notice" style={{ marginTop: 15, marginBottom: 0 }}>
                    <Icon name="warning" />
                    <span>{t.blocked_reason}</span>
                  </div>
                )}
              </div>
              <TaskExtras taskId={t.id} projectId={t.project.id} />
              <div className="drawer-section">
                <h3>Keep the conversation moving</h3>
                {(comments.data?.items ?? []).map((c) => (
                  <div className="comment" key={c.id}>
                    <Avatar user={c.author} size="sm" />
                    <div className="bubble">
                      <div className="row between">
                        <b className="tiny">{c.author?.display_name}</b>
                        <span className="tiny muted">{fmtDate(c.created_at)}</span>
                      </div>
                      <p>{c.body}</p>
                    </div>
                  </div>
                ))}
                {comments.data?.items.length === 0 && <p className="tiny muted">A good place for context and quick updates.</p>}
                {t.can_edit && (
                  <form onSubmit={post} style={{ marginTop: 17 }}>
                    <textarea value={comment} onChange={(e) => setComment(e.target.value)} placeholder="Add a thoughtful update…" aria-label="Add a comment" style={{ width: "100%" }} required maxLength={1500} />
                    <div className="row between" style={{ marginTop: 11 }}>
                      <span className="tiny muted">Visible to people with access to this task</span>
                      <button className="btn soft" type="submit" disabled={busy}>Post update <Icon name="send" className="sm" /></button>
                    </div>
                  </form>
                )}
              </div>
              {t.can_edit && (
                <div className="row between" style={{ marginTop: 22 }}>
                  {t.status === "DONE" ? (
                    <Button icon="check" variant="primary" onClick={() => setStatus(t, "TODO")}>Reopen task</Button>
                  ) : (
                    <Button icon="check" variant="primary" onClick={() => setStatus(t, "DONE")} disabled={t.status === "BLOCKED" || t.status === "CANCELLED"}>Mark complete</Button>
                  )}
                  {t.status !== "CANCELLED" && t.status !== "DONE" && <Button icon="trash" variant="danger" onClick={() => setStatus(t, "CANCELLED")}>Cancel task</Button>}
                </div>
              )}
            </>
          )}
        </section>
      </div>
    </div>
  );
}
