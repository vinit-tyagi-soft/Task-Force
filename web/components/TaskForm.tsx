"use client";

import { useEffect, useState, type FormEvent } from "react";
import { api, errorMessage } from "@/lib/api";
import { addDays, PRIORITIES, priorityLabel, STATUSES, statusLabel } from "@/lib/format";
import type { List, Project, Task, User } from "@/lib/types";
import { Modal } from "./Modal";
import { Notice } from "./ui";
import { useSession, type TaskFormDefaults } from "./session";

interface Values {
  title: string;
  project: string;
  owner: string;
  status: string;
  priority: string;
  start_on: string;
  due_on: string;
  estimate_hours: string;
  description: string;
  blocked_reason: string;
}

export function TaskForm({ taskId, defaults = {}, onClose }: { taskId?: string; defaults?: TaskFormDefaults; onClose: () => void }) {
  const { me, activeOrg, refresh, toast } = useSession();
  const [existing, setExisting] = useState<Task | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [members, setMembers] = useState<User[]>([]);
  const [values, setValues] = useState<Values | null>(null);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [idemKey] = useState(() => crypto.randomUUID());

  useEffect(() => {
    const spaces = [me.personal_workspace_id, activeOrg?.workspace_id].filter(Boolean) as string[];
    Promise.all([
      Promise.all(spaces.map((ws) => api<List<Project>>(`/workspaces/${ws}/projects`))),
      taskId ? api<Task>(`/tasks/${taskId}`) : Promise.resolve(null),
    ])
      .then(([lists, task]) => {
        const writable = lists.flatMap((l) => l.items).filter((p) => p.access === "CONTRIBUTE" || p.access === "MANAGE");
        setProjects(writable);
        setExisting(task);
        const today = me.today;
        const projectId = task?.project.id ?? (defaults.projectId && writable.some((p) => p.id === defaults.projectId) ? defaults.projectId : writable[0]?.id ?? "");
        setValues({
          title: task?.title ?? "",
          project: projectId,
          owner: task?.owner?.id ?? me.user.id,
          status: task?.status ?? defaults.status ?? "TODO",
          priority: task?.priority ?? "MEDIUM",
          start_on: task?.start_on ?? today,
          due_on: task?.due_on ?? defaults.dueOn ?? addDays(today, 3),
          estimate_hours: String(task?.estimate_hours ?? 2),
          description: task?.description ?? "",
          blocked_reason: task?.blocked_reason ?? "",
        });
      })
      .catch((e) => setError(errorMessage(e)));
    // Load once per open; defaults are fixed for the lifetime of the dialog.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId]);

  const project = projects.find((p) => p.id === values?.project);
  const isManager = project?.access === "MANAGE";
  useEffect(() => {
    if (!project || project.access !== "MANAGE") return;
    api<List<User>>(`/projects/${project.id}/members`).then((l) => setMembers(l.items)).catch(() => setMembers([]));
  }, [project]);

  const set = (k: keyof Values) => (e: { target: { value: string } }) => setValues((v) => (v ? { ...v, [k]: e.target.value } : v));

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!values) return;
    setSaving(true);
    setError("");
    const body = {
      title: values.title,
      description: values.description,
      status: values.status,
      priority: values.priority,
      owner_id: values.owner,
      start_on: values.start_on,
      due_on: values.due_on,
      estimate_hours: Number(values.estimate_hours),
      blocked_reason: values.blocked_reason,
    };
    try {
      if (existing) {
        await api<Task>(`/tasks/${existing.id}`, { method: "PATCH", body, version: existing.version });
        toast(`Updated ${values.title}`);
      } else {
        await api<Task>(`/projects/${values.project}/tasks`, { method: "POST", body, idempotencyKey: idemKey });
        toast(`Created ${values.title}`);
      }
      refresh();
      onClose();
    } catch (err) {
      setError(errorMessage(err));
      setSaving(false);
    }
  }

  const base = isManager && members.length ? members : [me.user];
  const ownerChoices = base.some((m) => m.id === values?.owner) || !existing?.owner ? base : [...base, existing.owner];

  return (
    <Modal title={existing ? "Edit your task" : "Make a little progress."} onClose={onClose} wide>
      {!values ? (
        error ? <Notice>{error}</Notice> : <p>Loading…</p>
      ) : projects.length === 0 && !existing ? (
        <Notice>You don’t have a project you can add tasks to yet. Create a personal project first.</Notice>
      ) : (
        <form onSubmit={submit}>
          <div className="form-grid">
            <label className="field full">
              Task title
              <input name="title" value={values.title} onChange={set("title")} required maxLength={180} />
            </label>
            <label className="field">
              Project & workspace
              <select value={values.project} onChange={set("project")} disabled={!!existing}>
                {existing && !project && <option value={existing.project.id}>{existing.project.name}</option>}
                {projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                    {p.workspace_kind === "PERSONAL" ? " · Private" : ""}
                  </option>
                ))}
              </select>
            </label>
            <label className="field">
              Accountable owner
              <select value={values.owner} onChange={set("owner")}>
                {ownerChoices.map((u) => (
                  <option key={u.id} value={u.id}>{u.display_name}</option>
                ))}
              </select>
            </label>
            <label className="field">
              Status
              <select value={values.status} onChange={set("status")}>
                {STATUSES.filter((s) => existing || s !== "CANCELLED").map((s) => (
                  <option key={s} value={s}>{statusLabel[s]}</option>
                ))}
              </select>
            </label>
            <label className="field">
              Priority
              <select value={values.priority} onChange={set("priority")}>
                {PRIORITIES.map((p) => (
                  <option key={p} value={p}>{priorityLabel[p]}</option>
                ))}
              </select>
            </label>
            <label className="field">
              Start date
              <input type="date" value={values.start_on} onChange={set("start_on")} required />
            </label>
            <label className="field">
              Due date
              <input type="date" value={values.due_on} onChange={set("due_on")} required />
            </label>
            <label className="field">
              Estimated effort (hours)
              <input type="number" min={0} max={500} step={0.5} value={values.estimate_hours} onChange={set("estimate_hours")} required />
            </label>
            <label className="field full">
              Description
              <textarea value={values.description} onChange={set("description")} maxLength={3000} />
            </label>
            <label className="field full">
              Dependency / blocked reason
              <textarea value={values.blocked_reason} onChange={set("blocked_reason")} maxLength={1000} />
            </label>
          </div>
          <div className="field-error" role="alert">{error}</div>
          <div className="modal-actions">
            <button type="button" className="btn" onClick={onClose}>Cancel</button>
            <button type="submit" className="btn primary" disabled={saving}>{existing ? "Save task" : "Create task"}</button>
          </div>
        </form>
      )}
    </Modal>
  );
}
