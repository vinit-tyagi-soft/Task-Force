"use client";

import { useState, type FormEvent } from "react";
import { api, errorMessage, useApi } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import type { List, User } from "@/lib/types";
import { Modal } from "@/components/Modal";
import { Badge, Button, Card, Empty, Heading, IconButton, LoadError, Loading, Notice, Tabs } from "@/components/ui";
import { useSession } from "@/components/session";

interface Token { id: string; name: string; prefix: string; scope: "read" | "write"; owner: User | null; expires_at: string; last_used_at: string | null; revoked_at: string | null; token?: string }
interface Webhook { id: string; name: string; url: string; events: string[]; enabled: boolean; version: number; secret?: string }
interface Delivery { id: string; endpoint: string; event: string; status: "PENDING" | "DELIVERED" | "FAILED"; attempts: number; response_code: number | null; error: string | null; created_at: string }
type Tab = "catalog" | "tokens" | "webhooks" | "logs";

const providers: [string, string, string, string, string][] = [
  ["Microsoft 365", "Mail, calendar & collaboration", "M", "#7090c4", "#eef3fb"],
  ["Google Workspace", "Gmail, calendar & shared context", "G", "#bc8d72", "#fbf3ec"],
  ["Slack", "Team updates where conversations happen", "#", "#a78cba", "#f7f0fb"],
  ["Jira", "Engineering issues & sprint context", "J", "#7199c9", "#edf3fb"],
  ["GitHub", "Pull requests, commits & release activity", "⌘", "#7b7f94", "#f0f1f7"],
];
const deliveryTone = { DELIVERED: "green", PENDING: "orange", FAILED: "red" } as const;

export default function IntegrationsPage() {
  const { activeOrg, refreshKey, refresh, toast, can } = useSession();
  const [tab, setTab] = useState<Tab>("catalog");
  const [modal, setModal] = useState<"token" | "webhook" | null>(null);
  const [revealed, setRevealed] = useState<{ label: string; value: string } | null>(null);
  const admin = can("integration.manage");
  const base = activeOrg && admin ? `/organizations/${activeOrg.id}` : null;
  const tokens = useApi<List<Token>>(base && tab === "tokens" ? `${base}/api-tokens` : null, refreshKey);
  const hooks = useApi<List<Webhook> & { events: string[] }>(base && `${base}/webhooks`, refreshKey);
  const logs = useApi<List<Delivery>>(base && tab === "logs" ? `${base}/webhook-deliveries` : null, refreshKey);

  if (!activeOrg) return <Empty title="No organization selected" desc="Integrations belong to an organization. Switch to one from the workspace menu." />;
  if (!base) return <Empty title="Integrations are managed by administrators" desc="Ask an organization owner or admin to connect tools or issue API tokens." />;

  async function act(fn: () => Promise<unknown>, done: string) {
    try {
      await fn();
      toast(done);
      refresh();
    } catch (err) {
      toast(errorMessage(err), "warning");
    }
  }
  const saveHook = (h: Webhook, patch: Partial<Webhook>) =>
    api(`/webhooks/${h.id}`, { method: "PUT", version: h.version, body: { name: h.name, url: h.url, events: h.events, enabled: h.enabled, ...patch } });

  let body;
  if (tab === "catalog")
    body = (
      <div className="grid3">
        {providers.map(([name, desc, mark, color, bg]) => (
          <article className="card integration-card" key={name}>
            <div className="row between">
              <span className="integration-logo" style={{ ["--provider" as string]: color, ["--provider-bg" as string]: bg }}>{mark}</span>
              <Badge label="Not connected" tone="gray" />
            </div>
            <h3 style={{ marginTop: 20 }}>{name}</h3>
            <p>{desc}</p>
            <Button disabled>Needs OAuth app setup</Button>
          </article>
        ))}
        <article className="card integration-card">
          <div className="row between">
            <span className="integration-logo" style={{ ["--provider" as string]: "#83a593", ["--provider-bg" as string]: "#eff6f2" }}>↗</span>
            <Badge label={hooks.data?.items.length ? `${hooks.data.items.length} endpoint${hooks.data.items.length === 1 ? "" : "s"}` : "Available"} tone={hooks.data?.items.length ? "green" : "blue"} />
          </div>
          <h3 style={{ marginTop: 20 }}>Custom webhooks</h3>
          <p>Signed workspace events for your own systems</p>
          <Button onClick={() => setTab("webhooks")}>Configure</Button>
        </article>
      </div>
    );
  else if (tab === "tokens")
    body = (
      <Card title="API tokens" sub="Each token acts as its creator inside this organization only — never personal workspaces, conversations or administration." extra={<Button icon="plus" onClick={() => setModal("token")}>New token</Button>}>
        {tokens.error ? <LoadError message={tokens.error.message} onRetry={tokens.reload} /> : !tokens.data ? <Loading /> : tokens.data.items.length ? (
          tokens.data.items.map((t) => (
            <div className="settings-row" key={t.id}>
              <div>
                <h3>{t.name} <Badge label={t.scope === "write" ? "Read & write" : "Read only"} tone={t.scope === "write" ? "orange" : "blue"} /></h3>
                <p>
                  <code>{t.prefix}…</code> · {t.owner?.display_name ?? "—"} · {t.revoked_at ? `Revoked ${fmtDate(t.revoked_at)}` : `Expires ${fmtDate(t.expires_at, { year: "numeric" })}`} · {t.last_used_at ? `Last used ${fmtDate(t.last_used_at)}` : "Never used"}
                </p>
              </div>
              {!t.revoked_at && <Button icon="trash" variant="danger" onClick={() => confirm(`Revoke ${t.name}? Systems using it stop working immediately.`) && act(() => api(`/api-tokens/${t.id}/revoke`, { method: "POST" }), `Revoked ${t.name}`)}>Revoke</Button>}
            </div>
          ))
        ) : (
          <Empty title="A deliberate connection starts here" desc="Create a scoped token for scripts and BI tools." />
        )}
      </Card>
    );
  else if (tab === "webhooks")
    body = (
      <Card title="Webhook endpoints" sub="Deliveries are signed with HMAC-SHA256 and retried up to 5 times with backoff." extra={<Button icon="plus" onClick={() => setModal("webhook")}>Add webhook</Button>}>
        {hooks.error ? <LoadError message={hooks.error.message} onRetry={hooks.reload} /> : !hooks.data ? <Loading /> : hooks.data.items.length ? (
          hooks.data.items.map((h) => (
            <div className="settings-row" key={h.id}>
              <div><h3>{h.name} {!h.enabled && <Badge label="Paused" tone="gray" />}</h3><p>{h.url} · {h.events.join(", ")}</p></div>
              <div className="row">
                <Button icon="play" onClick={() => act(() => api(`/webhooks/${h.id}/test`, { method: "POST" }), "Test event queued")}>Send test</Button>
                <button className={`toggle ${h.enabled ? "on" : ""}`} role="switch" aria-checked={h.enabled} aria-label={`${h.enabled ? "Pause" : "Resume"} ${h.name}`} onClick={() => act(() => saveHook(h, { enabled: !h.enabled }), h.enabled ? "Paused" : "Resumed")}><span /></button>
                <IconButton label={`Remove ${h.name}`} icon="trash" onClick={() => confirm(`Remove ${h.name}?`) && act(() => api(`/webhooks/${h.id}`, { method: "DELETE", version: h.version }), `Removed ${h.name}`)} />
              </div>
            </div>
          ))
        ) : (
          <Empty title="Keep downstream systems informed" desc="Add an HTTPS endpoint to receive task and project events." />
        )}
      </Card>
    );
  else
    body = (
      <Card title="Delivery log" sub="The latest 50 deliveries across your endpoints" extra={<button className="textbtn" onClick={logs.reload}>Refresh</button>}>
        {logs.error ? <LoadError message={logs.error.message} onRetry={logs.reload} /> : !logs.data ? <Loading /> : logs.data.items.length ? (
          logs.data.items.map((d) => (
            <div className="settings-row" key={d.id}>
              <div><h3>{d.endpoint} · {d.event}</h3><p>{new Date(d.created_at).toLocaleString("en-GB")} · {d.attempts} attempt{d.attempts === 1 ? "" : "s"}{d.response_code ? ` · HTTP ${d.response_code}` : ""}{d.error ? ` · ${d.error}` : ""}</p></div>
              <Badge label={d.status === "DELIVERED" ? "Delivered" : d.status === "FAILED" ? "Failed" : "Pending"} tone={deliveryTone[d.status]} />
            </div>
          ))
        ) : (
          <Empty title="No deliveries to inspect" desc="Send a test event or change a task to generate a delivery." />
        )}
      </Card>
    );

  return (
    <>
      <Heading title="A connected workspace, by design." sub="Thoughtful integrations for the tools your people already use." />
      <Notice>Use tokens with <code>Authorization: Bearer tf_pat_…</code> against <code>/api/v1</code>. Verify webhooks by computing HMAC-SHA256 of <code>timestamp.body</code> with your endpoint secret and comparing it to <code>X-TaskForce-Signature</code>.</Notice>
      <Tabs<Tab> items={[["catalog", "App catalog"], ["tokens", "API tokens"], ["webhooks", "Webhooks", hooks.data?.items.length], ["logs", "Delivery log"]]} active={tab} onChange={setTab} />
      {body}
      {modal === "token" && <TokenForm base={base} onClose={() => setModal(null)} onSecret={(v) => setRevealed({ label: "API token", value: v })} />}
      {modal === "webhook" && <WebhookForm base={base} events={hooks.data?.events ?? []} onClose={() => setModal(null)} onSecret={(v) => setRevealed({ label: "Signing secret", value: v })} />}
      {revealed && (
        <Modal title="Copy this now — it won’t be shown again." onClose={() => setRevealed(null)}>
          <p className="tiny muted">{revealed.label}</p>
          <input readOnly value={revealed.value} onFocus={(e) => e.target.select()} style={{ width: "100%", fontFamily: "monospace" }} aria-label={revealed.label} />
          <div className="modal-actions">
            <button className="btn" onClick={() => navigator.clipboard.writeText(revealed.value).then(() => toast("Copied"))}>Copy</button>
            <button className="btn primary" onClick={() => setRevealed(null)}>Done</button>
          </div>
        </Modal>
      )}
    </>
  );
}

function useSubmit(onClose: () => void) {
  const { refresh, toast } = useSession();
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  async function run<T>(fn: () => Promise<T>, done: string, after: (v: T) => void) {
    setSaving(true);
    setError("");
    try {
      const v = await fn();
      toast(done);
      refresh();
      onClose();
      after(v);
    } catch (err) {
      setError(errorMessage(err));
      setSaving(false);
    }
  }
  return { error, saving, run };
}

function TokenForm({ base, onClose, onSecret }: { base: string; onClose: () => void; onSecret: (v: string) => void }) {
  const [v, setV] = useState({ name: "", scope: "read", expires_in_days: 90 });
  const { error, saving, run } = useSubmit(onClose);
  function submit(e: FormEvent) {
    e.preventDefault();
    run(() => api<Token>(`${base}/api-tokens`, { method: "POST", body: v }), `Created ${v.name}`, (t) => onSecret(t.token ?? ""));
  }
  return (
    <Modal title="Issue a scoped API token." onClose={onClose}>
      <form onSubmit={submit}>
        <div className="form-grid">
          <label className="field full">Token name<input value={v.name} onChange={(e) => setV((x) => ({ ...x, name: e.target.value }))} required maxLength={80} placeholder="e.g. BI dashboard sync" /></label>
          <label className="field">
            Access
            <select value={v.scope} onChange={(e) => setV((x) => ({ ...x, scope: e.target.value }))}>
              <option value="read">Read only</option>
              <option value="write">Read & write tasks</option>
            </select>
          </label>
          <label className="field">
            Expires after
            <select value={v.expires_in_days} onChange={(e) => setV((x) => ({ ...x, expires_in_days: Number(e.target.value) }))}>
              {[7, 30, 90, 365].map((d) => <option key={d} value={d}>{d} days</option>)}
            </select>
          </label>
        </div>
        <div className="field-error" role="alert">{error}</div>
        <div className="modal-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={saving}>Create token</button>
        </div>
      </form>
    </Modal>
  );
}

function WebhookForm({ base, events, onClose, onSecret }: { base: string; events: string[]; onClose: () => void; onSecret: (v: string) => void }) {
  const [v, setV] = useState({ name: "", url: "https://" });
  const [picked, setPicked] = useState<Set<string>>(new Set(["task.status_changed"]));
  const { error, saving, run } = useSubmit(onClose);
  function submit(e: FormEvent) {
    e.preventDefault();
    run(() => api<Webhook>(`${base}/webhooks`, { method: "POST", body: { ...v, events: [...picked] } }), `Added ${v.name}`, (h) => onSecret(h.secret ?? ""));
  }
  return (
    <Modal title="Send events to your systems." onClose={onClose}>
      <form onSubmit={submit}>
        <div className="form-grid">
          <label className="field full">Endpoint name<input value={v.name} onChange={(e) => setV((x) => ({ ...x, name: e.target.value }))} required maxLength={80} /></label>
          <label className="field full">HTTPS URL<input type="url" value={v.url} onChange={(e) => setV((x) => ({ ...x, url: e.target.value }))} required maxLength={500} pattern="https://.*" /></label>
          <fieldset className="field full" style={{ border: 0, padding: 0, margin: 0 }}>
            <legend>Events</legend>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 6 }}>
              {events.map((ev) => (
                <label key={ev} className="row" style={{ fontWeight: 400 }}>
                  <input type="checkbox" checked={picked.has(ev)} onChange={() => setPicked((s) => { const n = new Set(s); if (n.has(ev)) n.delete(ev); else n.add(ev); return n; })} />
                  <code>{ev}</code>
                </label>
              ))}
            </div>
          </fieldset>
        </div>
        <div className="field-error" role="alert">{error}</div>
        <div className="modal-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={saving || picked.size === 0}>Add webhook</button>
        </div>
      </form>
    </Modal>
  );
}
