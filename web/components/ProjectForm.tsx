"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { api, errorMessage } from "@/lib/api";
import { addDays } from "@/lib/format";
import type { List, Project } from "@/lib/types";
import { Modal } from "./Modal";
import { useSession } from "./session";

export function ProjectForm({ workspaceId, personal, projectId, onClose }: { workspaceId: string; personal: boolean; projectId?: string; onClose: () => void }) {
  const { me, activeOrg, refresh, toast } = useSession();
  const router = useRouter();
  const [existing, setExisting] = useState<Project | null>(null);
  const [departments, setDepartments] = useState<{ id: string; name: string }[]>([]);
  const [v, setV] = useState({ name: "", description: "", department_id: "", start_on: me.today, due_on: addDays(me.today, 21), color: "#7964da" });
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!personal && activeOrg) {
      api<List<{ id: string; name: string }>>(`/organizations/${activeOrg.id}/departments`).then((l) => {
        setDepartments(l.items);
        setV((x) => ({ ...x, department_id: x.department_id || l.items[0]?.id || "" }));
      });
    }
    if (projectId) {
      api<Project>(`/projects/${projectId}`).then((p) => {
        setExisting(p);
        setV({ name: p.name, description: p.description, department_id: p.department_id ?? "", start_on: p.start_on, due_on: p.due_on, color: p.color });
      });
    }
  }, [personal, activeOrg, projectId]);

  const set = (k: keyof typeof v) => (e: { target: { value: string } }) => setV((x) => ({ ...x, [k]: e.target.value }));

  async function submit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError("");
    try {
      if (existing) {
        await api(`/projects/${existing.id}`, {
          method: "PATCH",
          version: existing.version,
          body: { name: v.name, description: v.description, start_on: v.start_on, due_on: v.due_on, color: v.color },
        });
        toast("Updated project");
        refresh();
        onClose();
      } else {
        const p = await api<Project>(`/workspaces/${workspaceId}/projects`, {
          method: "POST",
          body: { name: v.name, description: v.description, start_on: v.start_on, due_on: v.due_on, color: v.color, ...(personal || !v.department_id ? {} : { department_id: v.department_id }) },
        });
        toast(`Created ${p.name}`);
        refresh();
        onClose();
        router.push(`/projects/${p.id}`);
      }
    } catch (err) {
      setError(errorMessage(err));
      setSaving(false);
    }
  }

  async function toggleArchive() {
    if (!existing) return;
    try {
      await api(`/projects/${existing.id}`, { method: "PATCH", version: existing.version, body: { state: existing.state === "ACTIVE" ? "ARCHIVED" : "ACTIVE" } });
      toast(existing.state === "ACTIVE" ? `Archived ${existing.name}` : `Restored ${existing.name}`);
      refresh();
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  return (
    <Modal title={existing ? "Refine your project" : "Bring a new idea to life."} onClose={onClose}>
      <form onSubmit={submit}>
        <div className="form-grid">
          <label className="field full">
            Project name
            <input value={v.name} onChange={set("name")} required maxLength={100} />
          </label>
          <label className="field full">
            What will this project achieve?
            <textarea value={v.description} onChange={set("description")} required maxLength={1000} />
          </label>
          {!personal && (
            <label className="field">
              Department
              <select value={v.department_id} onChange={set("department_id")} disabled={!!existing}>
                {departments.map((d) => (
                  <option key={d.id} value={d.id}>{d.name}</option>
                ))}
              </select>
            </label>
          )}
          <label className="field">
            Workspace
            <select disabled value={personal ? "personal" : "org"}>
              <option value="org">{activeOrg?.name ?? "Organization"}</option>
              <option value="personal">Private personal workspace</option>
            </select>
          </label>
          <label className="field">
            Start date
            <input type="date" value={v.start_on} onChange={set("start_on")} required />
          </label>
          <label className="field">
            Target delivery
            <input type="date" value={v.due_on} onChange={set("due_on")} required />
          </label>
          <label className="field">
            Accent color
            <input type="color" value={v.color} onChange={set("color")} />
          </label>
        </div>
        <div className="field-error" role="alert">{error}</div>
        <div className="modal-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={saving}>{existing ? "Save project" : "Create project"}</button>
        </div>
      </form>
      {existing && (
        <div style={{ marginTop: 10 }}>
          <button className="textbtn" onClick={toggleArchive}>{existing.state === "ACTIVE" ? "Archive project" : "Restore project"}</button>
        </div>
      )}
    </Modal>
  );
}
