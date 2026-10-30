"use client";

import { useState, type FormEvent } from "react";
import { api, errorMessage, useApi } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import type { List, User } from "@/lib/types";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Badge, Button, Card, Empty, Heading, IconButton, LoadError, Loading, Notice, Tabs } from "@/components/ui";
import { useSession } from "@/components/session";

type Trigger = "TASK_OVERDUE" | "TASK_BLOCKED" | "TASK_DUE_SOON";
type Action = "NOTIFY_TASK_OWNER" | "NOTIFY_PROJECT_OWNER" | "NOTIFY_PROJECT_MANAGERS";
interface Rule { id: string; name: string; trigger: Trigger; action: Action; enabled: boolean; last_run_at: string | null; version: number }
interface Run { id: string; rule_name: string; started_by: User | null; matched: number; notified: number; created_at: string }
interface Alert { id: string; title: string; detail: string; task_id: string | null; created_at: string; read: boolean }
type Tab = "rules" | "alerts" | "runs";

const triggerLabel: Record<Trigger, string> = {
  TASK_OVERDUE: "A task is overdue",
  TASK_BLOCKED: "A task is blocked",
  TASK_DUE_SOON: "Urgent or high priority work is due within 2 days",
};
const actionLabel: Record<Action, string> = {
  NOTIFY_TASK_OWNER: "Notify the task owner",
  NOTIFY_PROJECT_OWNER: "Notify the project owner",
  NOTIFY_PROJECT_MANAGERS: "Notify project owner and managers",
};

export default function AutomationsPage() {
  const { activeOrg, refreshKey, refresh, toast, can, openTask } = useSession();
  const admin = can("automation.manage");
  const [tab, setTab] = useState<Tab>(admin ? "rules" : "alerts");
  const [form, setForm] = useState<{ rule?: Rule } | null>(null);
  const [running, setRunning] = useState(false);
  const base = activeOrg ? `/organizations/${activeOrg.id}` : null;
  const alerts = useApi<List<Alert> & { unread: number }>(base && `${base}/notifications`, refreshKey);
  const rules = useApi<List<Rule>>(base && admin ? `${base}/automation-rules` : null, refreshKey);
  const runs = useApi<List<Run>>(base && admin && tab === "runs" ? `${base}/automation-runs` : null, refreshKey);

  if (!activeOrg) return <Empty title="No organization selected" desc="Automations run inside an organization. Switch to one from the workspace menu." />;
  if (alerts.error) return <LoadError message={alerts.error.message} onRetry={alerts.reload} />;
  if (!alerts.data || (admin && !rules.data)) return <Loading />;

  async function act(fn: () => Promise<unknown>, done: string) {
    try {
      await fn();
      toast(done);
      refresh();
    } catch (err) {
      toast(errorMessage(err), "warning");
    }
  }

  async function runNow() {
    setRunning(true);
    try {
      const res = await api<List<{ notified: number }>>(`${base}/automation-runs`, { method: "POST" });
      const total = res.items.reduce((n, r) => n + r.notified, 0);
      toast(`Ran ${res.items.length} rule${res.items.length === 1 ? "" : "s"} · ${total} new alert${total === 1 ? "" : "s"}`);
      refresh();
    } catch (err) {
      toast(errorMessage(err), "warning");
    }
    setRunning(false);
  }

  const toggle = (r: Rule) =>
    act(() => api(`/automation-rules/${r.id}`, { method: "PUT", version: r.version, body: { name: r.name, trigger: r.trigger, action: r.action, enabled: !r.enabled } }), `${r.enabled ? "Paused" : "Enabled"} ${r.name}`);

  let body;
  if (tab === "alerts")
    body = (
      <Card title="Alert inbox" extra={alerts.data.unread > 0 && <button className="textbtn" onClick={() => act(() => api(`${base}/notifications/read`, { method: "POST", body: { all: true } }), "All caught up")}>Mark all read</button>}>
        {alerts.data.items.length ? (
          alerts.data.items.map((n) => (
            <div key={n.id} className={`notification-item ${n.read ? "" : "unread"}`}>
              <span className={`metric-icon ${n.read ? "blue" : "orange"}`}><Icon name={n.read ? "check" : "bell"} /></span>
              <div style={{ flex: 1 }}>
                <button className="task-title" onClick={() => n.task_id && openTask(n.task_id)}>{n.title}</button>
                <p>{n.detail} · {fmtDate(n.created_at)}</p>
              </div>
              {!n.read && <IconButton label="Mark read" icon="check" onClick={() => act(() => api(`${base}/notifications/read`, { method: "POST", body: { ids: [n.id] } }), "Marked read")} />}
            </div>
          ))
        ) : (
          <Empty title="All clear for now" desc="New alerts from workflow rules will appear here." />
        )}
      </Card>
    );
  else if (tab === "runs")
    body = (
      <Card title="Run history">
        {runs.error ? <LoadError message={runs.error.message} onRetry={runs.reload} /> : !runs.data ? <Loading /> : runs.data.items.length ? (
          runs.data.items.map((r) => (
            <div className="settings-row" key={r.id}>
              <div><h3>{r.rule_name}</h3><p>{new Date(r.created_at).toLocaleString("en-GB")} · {r.matched} matching tasks · {r.notified} new alerts · by {r.started_by?.display_name ?? "—"}</p></div>
              <Badge label="Completed" tone="green" />
            </div>
          ))
        ) : (
          <Empty title="No rules evaluated yet" desc="Run your rules to see what they match." />
        )}
      </Card>
    );
  else
    body = rules.data!.items.length ? (
      <div className="rules-grid">
        {rules.data!.items.map((r) => (
          <article className="card rule-card" key={r.id}>
            <div className="row between">
              <span className="metric-icon"><Icon name="zap" /></span>
              <button className={`toggle ${r.enabled ? "on" : ""}`} role="switch" aria-checked={r.enabled} aria-label={`${r.enabled ? "Pause" : "Enable"} ${r.name}`} onClick={() => toggle(r)}><span /></button>
            </div>
            <h3>{r.name}</h3>
            <div className="rule-flow">
              <div className="rule-step"><span>WHEN</span>{triggerLabel[r.trigger]}</div>
              <div style={{ textAlign: "center", color: "var(--muted)" }}>↓</div>
              <div className="rule-step"><span>THEN</span>{actionLabel[r.action]}</div>
            </div>
            <div className="row between" style={{ marginTop: 20 }}>
              <span className="tiny muted">{r.last_run_at ? `Last run: ${fmtDate(r.last_run_at)}` : "Not run yet"}</span>
              <button className="textbtn" onClick={() => setForm({ rule: r })}>Edit rule</button>
            </div>
          </article>
        ))}
      </div>
    ) : (
      <Empty title="Make room for a smarter routine" desc="Create a rule to get started." />
    );

  return (
    <>
      <Heading
        title="Little routines. Meaningful momentum."
        sub="Bring the right signal to the right person, at the right moment."
        actions={admin && (
          <>
            <Button icon="play" onClick={runNow} disabled={running}>{running ? "Running…" : "Run rules now"}</Button>
            <Button icon="plus" variant="primary" onClick={() => setForm({})}>Create rule</Button>
          </>
        )}
      />
      <Notice>Rules run when an administrator selects “Run rules now”. Alerts reach only people who can already see the task, and the same alert is never repeated on the same day.</Notice>
      <Tabs<Tab>
        items={admin ? [["rules", "Workflow rules", rules.data!.items.length], ["alerts", "Alert inbox", alerts.data.unread], ["runs", "Run history"]] : [["alerts", "Alert inbox", alerts.data.unread]]}
        active={tab}
        onChange={setTab}
      />
      {body}
      {form && base && <RuleForm base={base} rule={form.rule} onClose={() => setForm(null)} />}
    </>
  );
}

function RuleForm({ base, rule, onClose }: { base: string; rule?: Rule; onClose: () => void }) {
  const { refresh, toast } = useSession();
  const [v, setV] = useState({ name: rule?.name ?? "", trigger: rule?.trigger ?? "TASK_OVERDUE", action: rule?.action ?? "NOTIFY_TASK_OWNER", enabled: rule?.enabled ?? true });
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

  function submit(e: FormEvent) {
    e.preventDefault();
    run(
      () => (rule ? api(`/automation-rules/${rule.id}`, { method: "PUT", version: rule.version, body: v }) : api(`${base}/automation-rules`, { method: "POST", body: v })),
      rule ? `Updated ${v.name}` : `Created ${v.name}`,
    );
  }

  return (
    <Modal title={rule ? "Refine this routine" : "Create a helpful routine."} onClose={onClose}>
      <form onSubmit={submit}>
        <div className="form-grid">
          <label className="field full">
            Rule name
            <input value={v.name} onChange={(e) => setV((x) => ({ ...x, name: e.target.value }))} required maxLength={80} />
          </label>
          <label className="field full">
            When
            <select value={v.trigger} onChange={(e) => setV((x) => ({ ...x, trigger: e.target.value as Trigger }))}>
              {(Object.keys(triggerLabel) as Trigger[]).map((k) => <option key={k} value={k}>{triggerLabel[k]}</option>)}
            </select>
          </label>
          <label className="field full">
            Then
            <select value={v.action} onChange={(e) => setV((x) => ({ ...x, action: e.target.value as Action }))}>
              {(Object.keys(actionLabel) as Action[]).map((k) => <option key={k} value={k}>{actionLabel[k]}</option>)}
            </select>
          </label>
          <label className="field row" style={{ fontWeight: 400 }}>
            <input type="checkbox" checked={v.enabled} onChange={(e) => setV((x) => ({ ...x, enabled: e.target.checked }))} />
            Enabled
          </label>
        </div>
        <div className="field-error" role="alert">{error}</div>
        <div className="modal-actions">
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn primary" disabled={saving}>{rule ? "Save rule" : "Create rule"}</button>
        </div>
      </form>
      {rule && (
        <div style={{ marginTop: 10 }}>
          <button className="textbtn" disabled={saving} onClick={() => confirm(`Delete ${rule.name}? Its run history is removed too.`) && run(() => api(`/automation-rules/${rule.id}`, { method: "DELETE", version: rule.version }), `Deleted ${rule.name}`)}>
            Delete rule
          </button>
        </div>
      )}
    </Modal>
  );
}
