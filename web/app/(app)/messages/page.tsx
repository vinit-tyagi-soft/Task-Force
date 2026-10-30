"use client";

import { useState, type FormEvent } from "react";
import { api, errorMessage, useApi } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import type { List, Project, User } from "@/lib/types";
import { Modal } from "@/components/Modal";
import { Avatar, Badge, Button, Empty, Heading, IconButton, LoadError, Loading, Notice } from "@/components/ui";
import { useSession } from "@/components/session";

interface Message { id: string; author: User | null; body: string; created_at: string }
interface Thread {
  id: string;
  subject: string;
  project: { id: string; name: string; color: string } | null;
  participants: User[];
  last_author: User | null;
  excerpt: string;
  last_message_at: string;
  message_count: number;
  unread: boolean;
  starred: boolean;
  messages?: Message[];
}
type Filter = "inbox" | "unread" | "starred";

const when = (iso: string) => new Date(iso).toLocaleString("en-GB", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });

export default function MessagesPage() {
  const { me, activeOrg, refreshKey, refresh, toast, openTaskForm } = useSession();
  const [filter, setFilter] = useState<Filter>("inbox");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [composing, setComposing] = useState(false);
  const [reply, setReply] = useState("");
  const [sending, setSending] = useState(false);
  const threads = useApi<List<Thread> & { unread: number }>(activeOrg && `/organizations/${activeOrg.id}/threads?filter=${filter}`, refreshKey);
  const currentId = threads.data?.items.some((t) => t.id === selectedId) ? selectedId : threads.data?.items[0]?.id ?? null;
  const thread = useApi<Thread>(currentId && `/threads/${currentId}`, refreshKey);

  if (!activeOrg) return <Empty title="No organization selected" desc="Conversations happen inside an organization. Switch to one from the workspace menu." />;
  if (threads.error) return <LoadError message={threads.error.message} onRetry={threads.reload} />;
  if (!threads.data) return <Loading />;

  async function act(fn: () => Promise<unknown>, done?: string) {
    try {
      await fn();
      if (done) toast(done);
      refresh();
    } catch (err) {
      toast(errorMessage(err), "warning");
    }
  }

  function open(t: Thread) {
    setSelectedId(t.id);
    setReply("");
    if (t.unread) act(() => api(`/threads/${t.id}/state`, { method: "PATCH", body: { read: true } }));
  }

  async function send(e: FormEvent) {
    e.preventDefault();
    if (!currentId || !reply.trim()) return;
    setSending(true);
    await act(() => api(`/threads/${currentId}/messages`, { method: "POST", body: { body: reply } }), "Reply sent");
    setReply("");
    setSending(false);
  }

  const t = thread.data?.id === currentId ? thread.data : null;
  const lastMessage = t?.messages?.[t.messages.length - 1];

  return (
    <>
      <Heading
        title="Conversations, connected to progress."
        sub="Keep the context. Turn a good conversation into a clear next step."
        actions={<Button icon="plus" variant="primary" onClick={() => setComposing(true)}>New conversation</Button>}
      />
      <Notice>Internal conversations in {activeOrg.name}. Only the people in a thread can read it — administrators included. External mail connectors arrive with API & Integrations.</Notice>
      <section className="card mail-layout">
        <div className="mail-list">
          <div className="mail-list-header">
            <div className="row between">
              <h2>Your conversations</h2>
              <Badge label={`${threads.data.unread} unread`} tone="gray" />
            </div>
            <select aria-label="Conversation view" value={filter} onChange={(e) => setFilter(e.target.value as Filter)}>
              <option value="inbox">All conversations</option>
              <option value="unread">Unread</option>
              <option value="starred">Starred</option>
            </select>
          </div>
          {threads.data.items.map((x) => (
            <button key={x.id} className={`mail-item ${x.id === currentId ? "selected" : ""} ${x.unread ? "unread" : ""}`} onClick={() => open(x)}>
              <div className="row between">
                <strong>{x.last_author?.display_name ?? "—"}</strong>
                <span className="tiny muted">{fmtDate(x.last_message_at)}</span>
              </div>
              <h4>{x.starred ? "☆ " : ""}{x.subject}</h4>
              <p className="ellipsis">{x.excerpt}</p>
            </button>
          ))}
          {!threads.data.items.length && <p className="tiny muted" style={{ padding: 16 }}>Nothing here yet.</p>}
        </div>
        <div className="mail-content">
          {!currentId ? (
            <Empty title="A quieter inbox" desc="Start a conversation with your team." />
          ) : !t ? (
            thread.error ? <LoadError message={thread.error.message} onRetry={thread.reload} /> : <Loading />
          ) : (
            <>
              <div className="row between" style={{ marginBottom: 20 }}>
                <span className="tiny muted">{t.participants.map((p) => p.display_name).join(", ")}</span>
                <div className="row">
                  <IconButton label={t.starred ? "Unstar conversation" : "Star conversation"} icon="star" onClick={() => act(() => api(`/threads/${t.id}/state`, { method: "PATCH", body: { starred: !t.starred } }))} />
                  <IconButton label="Mark as unread" icon="mail" onClick={() => act(() => api(`/threads/${t.id}/state`, { method: "PATCH", body: { read: false } }), "Marked as unread")} />
                </div>
              </div>
              <h2>{t.subject}</h2>
              {t.messages?.map((m) => (
                <div key={m.id} style={{ marginTop: 18 }}>
                  <div className="row">
                    <Avatar user={m.author} />
                    <div><h3>{m.author?.display_name ?? "Former member"}</h3><p className="tiny muted">{m.author?.id === me.user.id ? "You" : m.author?.title}</p></div>
                    <span className="spacer" />
                    <span className="tiny muted">{when(m.created_at)}</span>
                  </div>
                  <div className="mail-body">{m.body}</div>
                </div>
              ))}
              {t.project && <Notice icon="link">Linked project: <b>{t.project.name}</b></Notice>}
              <div className="mail-toolbar">
                <Button
                  icon="checkSquare"
                  variant="primary"
                  onClick={() => openTaskForm({ defaults: { projectId: t.project?.id, title: t.subject.slice(0, 200), description: lastMessage ? `From conversation with ${lastMessage.author?.display_name ?? "a colleague"}:\n\n${lastMessage.body}`.slice(0, 2000) : "" } })}
                >
                  Review & create task
                </Button>
              </div>
              <form onSubmit={send} style={{ marginTop: 16 }}>
                <textarea value={reply} onChange={(e) => setReply(e.target.value)} placeholder="Write a reply…" aria-label="Reply" maxLength={10000} style={{ width: "100%", minHeight: 90 }} />
                <div className="modal-actions"><button type="submit" className="btn primary" disabled={sending || !reply.trim()}>Send reply</button></div>
              </form>
            </>
          )}
        </div>
      </section>
      {composing && <Compose orgId={activeOrg.id} workspaceId={activeOrg.workspace_id} onClose={() => setComposing(false)} onCreated={(id) => setSelectedId(id)} />}
    </>
  );
}

function Compose({ orgId, workspaceId, onClose, onCreated }: { orgId: string; workspaceId: string; onClose: () => void; onCreated: (id: string) => void }) {
  const { me, refresh, toast } = useSession();
  const members = useApi<List<User & { state: string }>>(`/organizations/${orgId}/members`);
  const projects = useApi<List<Project>>(`/workspaces/${workspaceId}/projects`);
  const [v, setV] = useState({ subject: "", body: "", project_id: "" });
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const set = (k: keyof typeof v) => (e: { target: { value: string } }) => setV((x) => ({ ...x, [k]: e.target.value }));

  async function submit(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError("");
    try {
      const t = await api<Thread>(`/organizations/${orgId}/threads`, {
        method: "POST",
        body: { subject: v.subject, body: v.body, participant_ids: [...picked], ...(v.project_id ? { project_id: v.project_id } : {}) },
      });
      toast("Conversation started");
      onCreated(t.id);
      refresh();
      onClose();
    } catch (err) {
      setError(errorMessage(err));
      setSaving(false);
    }
  }

  const people = (members.data?.items ?? []).filter((m) => m.state === "ACTIVE" && m.id !== me.user.id);
  return (
    <Modal title="Start a conversation." onClose={onClose}>
      <form onSubmit={submit}>
        <div className="form-grid">
          <label className="field full">
            Subject
            <input value={v.subject} onChange={set("subject")} required maxLength={160} />
          </label>
          <fieldset className="field full" style={{ border: 0, padding: 0, margin: 0 }}>
            <legend>People</legend>
            <div style={{ maxHeight: 150, overflow: "auto", display: "grid", gridTemplateColumns: "1fr 1fr", gap: 6 }}>
              {people.map((m) => (
                <label key={m.id} className="row" style={{ fontWeight: 400 }}>
                  <input
                    type="checkbox"
                    checked={picked.has(m.id)}
                    onChange={() => setPicked((s) => { const n = new Set(s); if (n.has(m.id)) n.delete(m.id); else n.add(m.id); return n; })}
                  />
                  {m.display_name}
                </label>
              ))}
            </div>
          </fieldset>
          <label className="field full">
            Linked project (optional)
            <select value={v.project_id} onChange={set("project_id")}>
              <option value="">No project</option>
              {projects.data?.items.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
          </label>
          <label className="field full">
            Message
            <textarea value={v.body} onChange={set("body")} required maxLength={10000} />
          </label>
        </div>
        <div className="field-error" role="alert">{error}</div>
        <div className="modal-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={saving || picked.size === 0}>Send</button>
        </div>
      </form>
    </Modal>
  );
}
