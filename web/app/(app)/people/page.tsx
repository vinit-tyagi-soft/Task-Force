"use client";

import { useEffect, useState, type FormEvent } from "react";
import { api, errorMessage, useApi } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import type { List, User } from "@/lib/types";
import { Modal } from "@/components/Modal";
import { Avatar, Badge, Button, Card, Empty, Heading, IconButton, LoadError, Loading, Notice, Progress, Tabs } from "@/components/ui";
import { useSession } from "@/components/session";

interface Candidate { id: string; requisition_id: string; full_name: string; email: string; stage: string; next_interview_at: string | null; notes: string; version: number }
interface Requisition { id: string; title: string; department_name: string; hiring_manager: User; openings: number; stage: string; version: number; candidates: Candidate[] }
interface Hiring extends List<Requisition> { can_manage: boolean; stages: string[]; candidate_stages: string[] }
interface Goal { id: string; title: string; target_on: string | null; progress: number }
type Tab = "hiring" | "development";

const label = (s: string) => s.charAt(0) + s.slice(1).toLowerCase().replaceAll("_", " ");
const stageTone = (s: string) => (["CLOSED", "REJECTED"].includes(s) ? "gray" : ["OFFER", "HIRED"].includes(s) ? "green" : s === "PENDING_APPROVAL" ? "orange" : "blue");

export default function PeoplePage() {
  const { activeOrg, refreshKey, refresh, toast } = useSession();
  const base = activeOrg ? `/organizations/${activeOrg.id}` : null;
  const hiring = useApi<Hiring>(base && `${base}/hiring`, refreshKey);
  const goals = useApi<List<Goal>>(base && `${base}/goals`, refreshKey);
  const [picked, setPicked] = useState<Tab | null>(null);
  const [form, setForm] = useState<{ kind: "requisition" } | { kind: "candidate"; req: Requisition } | null>(null);
  const [goal, setGoal] = useState({ title: "", target_on: "" });

  if (!activeOrg) return <Empty title="No organization selected" desc="Hiring and development live inside an organization. Switch to one from the workspace menu." />;
  if (hiring.error) return <LoadError message={hiring.error.message} onRetry={hiring.reload} />;
  if (!hiring.data || !goals.data) return <Loading />;
  const h = hiring.data;
  const showHiring = h.can_manage || h.items.length > 0;
  const tab: Tab = picked ?? (showHiring ? "hiring" : "development");

  async function act(fn: () => Promise<unknown>, done: string) {
    try {
      await fn();
      toast(done);
      refresh();
    } catch (err) {
      toast(errorMessage(err), "warning");
    }
  }

  function addGoal(e: FormEvent) {
    e.preventDefault();
    act(() => api(`${base}/goals`, { method: "POST", body: goal }), "Goal added").then(() => setGoal({ title: "", target_on: "" }));
  }

  const upcoming = h.items
    .flatMap((r) => r.candidates.filter((c) => c.next_interview_at && new Date(c.next_interview_at) >= new Date()).map((c) => ({ c, r })))
    .sort((a, b) => a.c.next_interview_at!.localeCompare(b.c.next_interview_at!));

  let body;
  if (tab === "hiring")
    body = (
      <>
        {h.items.length ? (
          <div className="grid3">
            {h.items.map((r) => (
              <Card key={r.id} title={r.title} extra={<Badge label={label(r.stage)} tone={stageTone(r.stage)} />}>
                <p style={{ fontSize: 11 }}>{r.department_name || "No department"} · {r.openings} opening{r.openings > 1 ? "s" : ""}</p>
                <div className="row between" style={{ marginTop: 16 }}>
                  <span className="row"><Avatar user={r.hiring_manager} size="sm" /><span className="tiny muted">Hiring manager</span></span>
                  {h.can_manage && (
                    <select aria-label={`Stage for ${r.title}`} value={r.stage} style={{ fontSize: 10 }}
                      onChange={(e) => act(() => api(`/requisitions/${r.id}`, { method: "PATCH", version: r.version, body: { stage: e.target.value } }), `${r.title} moved to ${label(e.target.value)}`)}>
                      {h.stages.map((s) => <option key={s} value={s}>{label(s)}</option>)}
                    </select>
                  )}
                </div>
                <div style={{ marginTop: 14 }}>
                  {r.candidates.map((c) => (
                    <div className="settings-row" key={c.id}>
                      <div>
                        <h3>{c.full_name}</h3>
                        <p>{c.next_interview_at ? `Interview ${new Date(c.next_interview_at).toLocaleString("en-GB", { dateStyle: "medium", timeStyle: "short" })}` : "No interview scheduled"}</p>
                      </div>
                      {h.can_manage ? (
                        <select aria-label={`Stage for ${c.full_name}`} value={c.stage} style={{ fontSize: 10 }}
                          onChange={(e) => act(() => api(`/candidates/${c.id}`, { method: "PATCH", version: c.version, body: { stage: e.target.value } }), `${c.full_name} moved to ${label(e.target.value)}`)}>
                          {h.candidate_stages.map((s) => <option key={s} value={s}>{label(s)}</option>)}
                        </select>
                      ) : (
                        <Badge label={label(c.stage)} tone={stageTone(c.stage)} />
                      )}
                    </div>
                  ))}
                  {!r.candidates.length && <p className="tiny muted">No candidates yet.</p>}
                </div>
                {h.can_manage && r.stage !== "CLOSED" && (
                  <button className="textbtn" style={{ marginTop: 12 }} onClick={() => setForm({ kind: "candidate", req: r })}>Add candidate</button>
                )}
              </Card>
            ))}
          </div>
        ) : (
          <Empty title="A fresh hiring pipeline" desc="Create your first hiring request." />
        )}
        <Card title="Upcoming interviews">
          {upcoming.length ? (
            upcoming.map(({ c, r }) => (
              <div className="settings-row" key={c.id}>
                <div><h3>{c.full_name}</h3><p>{r.title} · {new Date(c.next_interview_at!).toLocaleString("en-GB", { dateStyle: "medium", timeStyle: "short" })}</p></div>
                <Badge label={label(c.stage)} tone={stageTone(c.stage)} />
              </div>
            ))
          ) : (
            <p>No upcoming interviews.</p>
          )}
        </Card>
      </>
    );
  else
    body = (
      <Card title="My development goals">
        <form className="row" style={{ gap: 8, marginBottom: 14 }} onSubmit={addGoal}>
          <input aria-label="New goal" placeholder="What do you want to grow in?" value={goal.title} maxLength={160} required style={{ flex: 1 }}
            onChange={(e) => setGoal((g) => ({ ...g, title: e.target.value }))} />
          <input aria-label="Target date" type="date" value={goal.target_on} onChange={(e) => setGoal((g) => ({ ...g, target_on: e.target.value }))} />
          <button type="submit" className="btn primary">Add goal</button>
        </form>
        {goals.data.items.length ? (
          goals.data.items.map((g) => (
            <div className="settings-row" key={g.id}>
              <div style={{ flex: 1 }}>
                <h3>{g.title}</h3>
                <p>{g.target_on ? `Target ${fmtDate(g.target_on)}` : "No target date"}</p>
                <div style={{ maxWidth: 320, marginTop: 6 }}><Progress value={g.progress} /></div>
              </div>
              <select aria-label={`Progress for ${g.title}`} value={g.progress} style={{ fontSize: 10 }}
                onChange={(e) => act(() => api(`/goals/${g.id}`, { method: "PATCH", body: { progress: Number(e.target.value) } }), "Progress saved")}>
                {Array.from({ length: 11 }, (_, i) => i * 10).map((p) => <option key={p} value={p}>{p}%</option>)}
              </select>
              <IconButton label={`Delete ${g.title}`} icon="trash" onClick={() => confirm(`Delete “${g.title}”?`) && act(() => api(`/goals/${g.id}`, { method: "DELETE" }), "Goal deleted")} />
            </div>
          ))
        ) : (
          <Empty title="Make growth intentional" desc="Add a goal you want to work towards." />
        )}
      </Card>
    );

  return (
    <>
      <Heading
        title="Help good people do great work."
        sub="From the next hire to the next skill, make growth intentional."
        actions={h.can_manage && tab === "hiring" && <Button icon="plus" variant="primary" onClick={() => setForm({ kind: "requisition" })}>Hiring request</Button>}
      />
      <Notice>Hiring records are visible only to HR, organization administrators and each role’s hiring manager. Development goals are private to you.</Notice>
      <Tabs<Tab>
        items={showHiring ? [["hiring", "Hiring & interviews", h.items.length], ["development", "My development", goals.data.items.length]] : [["development", "My development", goals.data.items.length]]}
        active={tab}
        onChange={setPicked}
      />
      {body}
      {form?.kind === "requisition" && base && activeOrg && <RequisitionForm base={base} orgId={activeOrg.id} onClose={() => setForm(null)} />}
      {form?.kind === "candidate" && <CandidateForm req={form.req} onClose={() => setForm(null)} />}
    </>
  );
}

function useSubmit(onClose: () => void) {
  const { refresh, toast } = useSession();
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  async function run(fn: () => Promise<unknown>, done: string) {
    setSaving(true);
    setError("");
    try {
      await fn();
      toast(done);
      refresh();
      onClose();
    } catch (err) {
      setError(errorMessage(err));
      setSaving(false);
    }
  }
  return { error, saving, run };
}

function RequisitionForm({ base, orgId, onClose }: { base: string; orgId: string; onClose: () => void }) {
  const { error, saving, run } = useSubmit(onClose);
  const [departments, setDepartments] = useState<{ id: string; name: string }[]>([]);
  const [members, setMembers] = useState<(User & { state: string })[]>([]);
  const [v, setV] = useState({ title: "", department_id: "", hiring_manager_id: "", openings: 1 });

  useEffect(() => {
    api<List<{ id: string; name: string }>>(`/organizations/${orgId}/departments`).then((l) => setDepartments(l.items));
    api<List<User & { state: string }>>(`/organizations/${orgId}/members`).then((l) => setMembers(l.items.filter((m) => m.state === "ACTIVE")));
  }, [orgId]);

  function submit(e: FormEvent) {
    e.preventDefault();
    run(() => api(`${base}/requisitions`, { method: "POST", body: { ...v, department_id: v.department_id || null } }), `Opened ${v.title}`);
  }

  return (
    <Modal title="Open a hiring request" onClose={onClose}>
      <form onSubmit={submit}>
        <div className="form-grid">
          <label className="field full">Role title<input value={v.title} onChange={(e) => setV((x) => ({ ...x, title: e.target.value }))} required maxLength={120} /></label>
          <label className="field">Department
            <select value={v.department_id} onChange={(e) => setV((x) => ({ ...x, department_id: e.target.value }))}>
              <option value="">None</option>
              {departments.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
            </select>
          </label>
          <label className="field">Openings<input type="number" min={1} max={50} value={v.openings} onChange={(e) => setV((x) => ({ ...x, openings: Number(e.target.value) }))} /></label>
          <label className="field full">Hiring manager
            <select value={v.hiring_manager_id} onChange={(e) => setV((x) => ({ ...x, hiring_manager_id: e.target.value }))} required>
              <option value="">Choose a member</option>
              {members.map((m) => <option key={m.id} value={m.id}>{m.display_name}</option>)}
            </select>
          </label>
        </div>
        <div className="field-error" role="alert">{error}</div>
        <div className="modal-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={saving}>Open request</button>
        </div>
      </form>
    </Modal>
  );
}

function CandidateForm({ req, onClose }: { req: Requisition; onClose: () => void }) {
  const { error, saving, run } = useSubmit(onClose);
  const [v, setV] = useState({ full_name: "", email: "", interview: "", notes: "" });

  function submit(e: FormEvent) {
    e.preventDefault();
    const body = { full_name: v.full_name, email: v.email, notes: v.notes, next_interview_at: v.interview ? new Date(v.interview).toISOString() : "" };
    run(() => api(`/requisitions/${req.id}/candidates`, { method: "POST", body }), `Added ${v.full_name}`);
  }

  return (
    <Modal title={`Add a candidate · ${req.title}`} onClose={onClose}>
      <form onSubmit={submit}>
        <div className="form-grid">
          <label className="field">Full name<input value={v.full_name} onChange={(e) => setV((x) => ({ ...x, full_name: e.target.value }))} required maxLength={120} /></label>
          <label className="field">Email<input type="email" value={v.email} onChange={(e) => setV((x) => ({ ...x, email: e.target.value }))} maxLength={320} /></label>
          <label className="field full">Next interview<input type="datetime-local" value={v.interview} onChange={(e) => setV((x) => ({ ...x, interview: e.target.value }))} /></label>
          <label className="field full">Notes<textarea value={v.notes} onChange={(e) => setV((x) => ({ ...x, notes: e.target.value }))} maxLength={2000} /></label>
        </div>
        <div className="field-error" role="alert">{error}</div>
        <div className="modal-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={saving}>Add candidate</button>
        </div>
      </form>
    </Modal>
  );
}
