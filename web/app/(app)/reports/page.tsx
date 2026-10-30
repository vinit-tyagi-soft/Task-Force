"use client";

import { useState } from "react";
import { api, errorMessage, useApi } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import type { List, Metrics, Status, Task } from "@/lib/types";
import { LineChart, Donut } from "@/components/charts";
import { Icon } from "@/components/Icon";
import { Badge, Button, Card, Empty, Heading, LoadError, Loading, Metric, Tabs } from "@/components/ui";
import { TaskTable } from "@/components/TaskTable";
import { useSession } from "@/components/session";

type Period = "all" | "30" | "7";
interface Filters { period: Period; department_id?: string }
interface Report {
  organization: string;
  as_of: string;
  filters: Filters;
  metrics: Metrics;
  status_counts: Partial<Record<Status, number>>;
  trend: { date: string; planned: number; completed: number }[];
  total: number;
  items: Task[];
  risk: Task[];
  definitions: Record<string, string>;
  can_export: boolean;
}
interface SavedView { id: string; name: string; filters: Filters }
interface ExportRecord { id: string; name: string; row_count: number; created_at: string }
type Tab = "overview" | "risk" | "exports";

const periodLabel: Record<Period, string> = { all: "All records", "30": "Due in the last 30 days", "7": "Due in the last 7 days" };

export default function ReportsPage() {
  const { activeOrg, refreshKey, refresh, toast, can } = useSession();
  const [tab, setTab] = useState<Tab>("overview");
  const [filters, setFilters] = useState<Filters>({ period: "all" });
  const [showDefs, setShowDefs] = useState(false);
  const base = activeOrg && can("report.read") ? `/organizations/${activeOrg.id}` : null;
  const query = `period=${filters.period}${filters.department_id ? `&department_id=${filters.department_id}` : ""}`;
  const report = useApi<Report>(base && `${base}/reports/tasks?${query}`, refreshKey);
  const departments = useApi<List<{ id: string; name: string }>>(base && `${base}/departments`, refreshKey);
  const views = useApi<List<SavedView>>(base && `${base}/report-views`, refreshKey);
  const exports = useApi<List<ExportRecord>>(base && `${base}/reports/exports`, refreshKey);

  if (!activeOrg) return <Empty title="No organization selected" desc="Reports cover organization work. Switch to one from the workspace menu." />;
  if (!base) return <Empty title="Reports aren’t part of your role" desc="Ask an organization administrator if you need reporting access." />;
  if (report.error) return <LoadError message={report.error.message} onRetry={report.reload} />;
  if (!report.data) return <Loading />;
  const r = report.data;
  const m = r.metrics;
  const deptName = departments.data?.items.find((d) => d.id === filters.department_id)?.name ?? "All departments";

  async function saveView() {
    const name = window.prompt("Name this saved view", `${deptName} · ${periodLabel[filters.period]}`);
    if (!name) return;
    try {
      await api(`${base}/report-views`, { method: "POST", body: { name, ...filters } });
      toast("Saved view");
      refresh();
    } catch (err) {
      toast(errorMessage(err), "warning");
    }
  }

  async function deleteView(v: SavedView) {
    try {
      await api(`/report-views/${v.id}`, { method: "DELETE" });
      toast(`Removed ${v.name}`);
      refresh();
    } catch (err) {
      toast(errorMessage(err), "warning");
    }
  }

  function exportCsv() {
    const link = document.createElement("a");
    link.href = `/api/v1${base}/reports/tasks.csv?${query}`;
    link.download = "";
    link.click();
    toast("Preparing your CSV export");
    setTimeout(refresh, 1500);
  }

  const statuses = Object.entries(r.status_counts).flatMap(([s, n]) => Array<Status>(n ?? 0).fill(s as Status));

  let body;
  if (tab === "risk")
    body = <Card title="Risk register" sub="Blocked or overdue records in the current filter">{r.risk.length ? <TaskTable tasks={r.risk} /> : <Empty title="Clear for the moment" desc="Nothing blocked or overdue in this view." />}</Card>;
  else if (tab === "exports")
    body = (
      <Card title="Export history" sub="Your CSV downloads for this organization">
        {exports.data?.items.length ? (
          exports.data.items.map((e) => (
            <div className="settings-row" key={e.id}>
              <div><h3>{e.name}</h3><p>{new Date(e.created_at).toLocaleString("en-GB")} · {e.row_count} records</p></div>
              <Badge label="Downloaded" tone="green" />
            </div>
          ))
        ) : (
          <Empty title="Ready when you are" desc="Download a CSV to start your export history." />
        )}
      </Card>
    );
  else
    body = (
      <div className="dashboard-grid">
        <div>
          <Card title="Delivery, over time" sub="Planned vs completed over the last 7 days, filtered register">
            <LineChart points={r.trend} asOf={r.as_of} />
          </Card>
          <Card title="Task register" extra={<Badge label={`${r.total} records`} tone="gray" />}>
            {r.items.length ? <TaskTable tasks={r.items} /> : <Empty title="No records" desc="Try a wider period or another department." />}
            {r.total > r.items.length && <p className="tiny muted" style={{ marginTop: 10 }}>Showing the first {r.items.length}. Export CSV for all {r.total} records.</p>}
          </Card>
        </div>
        <div>
          <Card title="Status composition"><Donut statuses={statuses} /></Card>
          <Card title="Saved views" extra={<button className="textbtn" onClick={saveView}>+ Save view</button>}>
            {views.data?.items.length ? (
              views.data.items.map((v) => (
                <div className="settings-row" key={v.id}>
                  <div>
                    <h3>{v.name}</h3>
                    <p>{departments.data?.items.find((d) => d.id === v.filters.department_id)?.name ?? "All departments"} · {periodLabel[v.filters.period]}</p>
                  </div>
                  <div className="row">
                    <button className="textbtn" onClick={() => setFilters(v.filters)}>Open</button>
                    <button className="textbtn" onClick={() => deleteView(v)} aria-label={`Remove ${v.name}`}>Remove</button>
                  </div>
                </div>
              ))
            ) : (
              <p>Save this filter combination for your next review.</p>
            )}
          </Card>
        </div>
      </div>
    );

  return (
    <>
      <Heading
        title="Useful insights. Better conversations."
        sub="A shared understanding of progress, with the source records one click away."
        actions={
          <>
            <Button icon="plus" onClick={saveView}>Save view</Button>
            {r.can_export && <Button icon="download" variant="primary" onClick={exportCsv}>Export CSV</Button>}
          </>
        }
      />
      <div className="toolbar">
        <div className="filter-group">
          <select aria-label="Reporting period" value={filters.period} onChange={(e) => setFilters((f) => ({ ...f, period: e.target.value as Period }))}>
            {(Object.keys(periodLabel) as Period[]).map((p) => <option key={p} value={p}>{periodLabel[p]}</option>)}
          </select>
          <select aria-label="Report department" value={filters.department_id ?? ""} onChange={(e) => setFilters((f) => ({ ...f, department_id: e.target.value || undefined }))}>
            <option value="">All departments</option>
            {departments.data?.items.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
          </select>
          <span className="pill">{r.organization} · Company only</span>
        </div>
        <button className="textbtn row" onClick={() => setShowDefs((x) => !x)}>Metric definitions <Icon name="info" className="sm" /></button>
      </div>
      {showDefs && (
        <Card title="How these numbers work">
          {Object.entries(r.definitions).map(([k, v]) => (
            <p key={k} style={{ marginBottom: 8 }}><b className="tiny">{k.replace("_", " ").toUpperCase()}</b> · {v}</p>
          ))}
          <p className="tiny muted">As of {fmtDate(r.as_of, { year: "numeric" })} in the organization’s reporting timezone.</p>
        </Card>
      )}
      <div className="metrics">
        <Metric label="Eligible tasks" value={m.eligible} sub="Cancelled tasks excluded" icon="checkSquare" />
        <Metric label="Completion" value={`${m.rate_percent}%`} sub={`${m.done} tasks done`} icon="target" tone="green" />
        <Metric label="Overdue" value={m.overdue} sub="Due before today, not done" icon="clock" tone="orange" />
        <Metric label="Needs attention" value={m.attention} sub="Unique blocked + overdue records" icon="flag" tone="blue" />
      </div>
      <Tabs<Tab> items={[["overview", "Delivery overview"], ["risk", "Risk register", r.risk.length], ["exports", "Export history", exports.data?.items.length]]} active={tab} onChange={setTab} />
      {body}
    </>
  );
}
