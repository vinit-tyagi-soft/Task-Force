"use client";

import { useRef, useState, type FormEvent } from "react";
import { api, errorMessage, useApi } from "@/lib/api";
import { statusLabel } from "@/lib/format";
import type { List, Status, Task, User } from "@/lib/types";
import { Badge, IconButton } from "./ui";
import { useSession } from "./session";

interface Linked { id: string; key: string; title: string; status: Status }
interface Attachment { id: string; filename: string; size_bytes: number; uploaded_by: User | null; created_at: string; can_delete: boolean }
interface Details {
  checklist: { id: string; title: string; done: boolean }[];
  depends_on: Linked[];
  blocking: Linked[];
  attachments: Attachment[];
  can_edit: boolean;
  can_attach: boolean;
}

const size = (n: number) => (n < 1024 ? `${n} B` : n < 1 << 20 ? `${Math.round(n / 1024)} KB` : `${(n / (1 << 20)).toFixed(1)} MB`);

/** Checklist, dependencies and attachments for the task drawer. */
export function TaskExtras({ taskId, projectId }: { taskId: string; projectId: string }) {
  const { refreshKey, refresh, toast, openTask } = useSession();
  const details = useApi<Details>(`/tasks/${taskId}/details`, refreshKey);
  const projectTasks = useApi<List<Task>>(details.data?.can_edit ? `/projects/${projectId}/tasks` : null, refreshKey);
  const [item, setItem] = useState("");
  const [dep, setDep] = useState("");
  const [uploading, setUploading] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  if (!details.data) return null;
  const d = details.data;
  const done = d.checklist.filter((c) => c.done).length;
  const linked = new Set([taskId, ...d.depends_on.map((x) => x.id)]);
  const options = (projectTasks.data?.items ?? []).filter((x) => !linked.has(x.id));

  async function act(fn: () => Promise<unknown>, msg?: string) {
    try {
      await fn();
      if (msg) toast(msg);
      refresh();
      return true;
    } catch (err) {
      toast(errorMessage(err), "warning");
      return false;
    }
  }

  async function addItem(e: FormEvent) {
    e.preventDefault();
    if (await act(() => api(`/tasks/${taskId}/checklist`, { method: "POST", body: { title: item } }))) setItem("");
  }

  async function addDep(e: FormEvent) {
    e.preventDefault();
    if (await act(() => api(`/tasks/${taskId}/dependencies`, { method: "POST", body: { depends_on_task_id: dep } }), "Dependency added")) setDep("");
  }

  async function upload(file: File) {
    if (file.size > 5 << 20) {
      toast("Files can be up to 5 MB.", "warning");
      return;
    }
    const body = new FormData();
    body.append("file", file);
    setUploading(true);
    await act(() => api(`/tasks/${taskId}/attachments`, { method: "POST", body }), `Attached ${file.name}`);
    setUploading(false);
    if (fileRef.current) fileRef.current.value = "";
  }

  const link = (x: Linked) => (
    <div className="row between" key={x.id} style={{ padding: "5px 0" }}>
      <button className="textbtn" onClick={() => openTask(x.id)}>{x.key} · {x.title}</button>
      <Badge label={statusLabel[x.status]} tone={x.status === "DONE" ? "green" : x.status === "BLOCKED" ? "red" : "gray"} />
    </div>
  );

  return (
    <>
      <div className="drawer-section">
        <h3>Checklist {d.checklist.length > 0 && <span className="tiny muted">· {done}/{d.checklist.length}</span>}</h3>
        {d.checklist.map((c) => (
          <div className="checklist-item" key={c.id}>
            <input type="checkbox" checked={c.done} disabled={!d.can_edit} aria-label={c.title}
              onChange={(e) => act(() => api(`/checklist-items/${c.id}`, { method: "PATCH", body: { done: e.target.checked } }))} />
            <span className={c.done ? "done-title" : ""} style={{ flex: 1 }}>{c.title}</span>
            {d.can_edit && <IconButton label={`Remove ${c.title}`} icon="trash" onClick={() => act(() => api(`/checklist-items/${c.id}`, { method: "DELETE" }))} />}
          </div>
        ))}
        {!d.checklist.length && !d.can_edit && <p className="tiny muted">No checklist yet.</p>}
        {d.can_edit && (
          <form className="row" style={{ gap: 8, marginTop: 8 }} onSubmit={addItem}>
            <input aria-label="New checklist item" placeholder="Add a step" value={item} onChange={(e) => setItem(e.target.value)} required maxLength={200} style={{ flex: 1 }} />
            <button type="submit" className="btn">Add</button>
          </form>
        )}
      </div>
      <div className="drawer-section">
        <h3>Dependencies</h3>
        <div className="tiny muted">WAITING ON</div>
        {d.depends_on.map((x) => (
          <div className="row" key={x.id}>
            <div style={{ flex: 1 }}>{link(x)}</div>
            {d.can_edit && <IconButton label={`Remove dependency on ${x.title}`} icon="trash" onClick={() => act(() => api(`/tasks/${taskId}/dependencies/${x.id}`, { method: "DELETE" }), "Dependency removed")} />}
          </div>
        ))}
        {!d.depends_on.length && <p className="tiny muted">Nothing — this task is not waiting on other work.</p>}
        {d.blocking.length > 0 && (
          <>
            <div className="tiny muted" style={{ marginTop: 10 }}>BLOCKING</div>
            {d.blocking.map(link)}
          </>
        )}
        {d.can_edit && options.length > 0 && (
          <form className="row" style={{ gap: 8, marginTop: 8 }} onSubmit={addDep}>
            <select aria-label="Depends on" value={dep} onChange={(e) => setDep(e.target.value)} required style={{ flex: 1 }}>
              <option value="">Waits on another task in this project…</option>
              {options.map((x) => <option key={x.id} value={x.id}>{x.key} · {x.title}</option>)}
            </select>
            <button type="submit" className="btn">Link</button>
          </form>
        )}
      </div>
      <div className="drawer-section">
        <h3>Files</h3>
        {d.attachments.map((a) => (
          <div className="row between" key={a.id} style={{ padding: "5px 0" }}>
            <a className="textbtn" href={`/api/v1/attachments/${a.id}`} download={a.filename}>{a.filename}</a>
            <span className="row">
              <span className="tiny muted">{size(a.size_bytes)} · {a.uploaded_by?.display_name}</span>
              {a.can_delete && <IconButton label={`Delete ${a.filename}`} icon="trash" onClick={() => confirm(`Delete ${a.filename}?`) && act(() => api(`/attachments/${a.id}`, { method: "DELETE" }), "File removed")} />}
            </span>
          </div>
        ))}
        {!d.attachments.length && <p className="tiny muted">No files attached.</p>}
        {d.can_attach && (
          <label className="btn" style={{ marginTop: 8 }}>
            {uploading ? "Uploading…" : "Attach a file (up to 5 MB)"}
            <input ref={fileRef} type="file" hidden disabled={uploading} onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])} />
          </label>
        )}
      </div>
    </>
  );
}
