"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useApi } from "@/lib/api";
import { fmtDate } from "@/lib/format";
import type { ExecutiveSummary } from "@/lib/types";
import { Icon } from "@/components/Icon";
import { Avatar, Card, Empty, HealthBadge, Heading, LoadError, Loading, Metric, Notice, Progress, ProjectIcon, StatusBadge } from "@/components/ui";
import { LineChart } from "@/components/charts";
import { useSession } from "@/components/session";

export default function ExecutiveDashboard() {
  const { orgId } = useParams<{ orgId: string }>();
  const { refreshKey, openTask } = useSession();
  const res = useApi<ExecutiveSummary>(`/organizations/${orgId}/executive-summary`, refreshKey);

  if (res.error) {
    const msg = res.error.status === 403 ? "Your role doesn’t include executive reporting for this organization." : res.error.status === 404 ? "This organization isn’t available to you." : res.error.message;
    return <LoadError message={msg} />;
  }
  if (!res.data) return <Loading />;

  const s = res.data;
  const m = s.metrics;
  return (
    <>
      <Heading title="The bigger picture. A clearer direction." sub="A considered view of portfolio health, delivery, and the decisions that matter." />
      <Notice>
        Showing <b>{s.organization.name}</b> only, as of {fmtDate(s.as_of, { year: "numeric" })} ({s.organization.timezone}). Private personal work never enters company metrics.
      </Notice>
      <div className="metrics">
        <Metric label="Active projects" value={s.projects.length} sub={`${s.projects_at_risk} need a closer look`} icon="folder" />
        <Metric label="Delivery completion" value={`${m.rate_percent}%`} sub={`${m.done} of ${m.eligible} eligible tasks`} icon="target" tone="green" />
        <Metric label="Needs attention" value={m.attention} sub="Unique overdue + blocked tasks" icon="warning" tone="orange" />
        <Metric label="Open delivery effort" value={`${m.open_hours}h`} sub={`${m.open} open company tasks`} icon="clock" tone="blue" />
      </div>
      <div className="dashboard-grid">
        <div>
          <Card title="Portfolio health">
            <div className="table-wrap">
              <table>
                <thead><tr><th>Project</th><th>Owner</th><th>Health</th><th>Completion</th><th>Target</th></tr></thead>
                <tbody>
                  {s.projects.map((p) => (
                    <tr key={p.id}>
                      <td><div className="row"><ProjectIcon project={p} /><Link className="task-title" style={{ maxWidth: 240 }} href={`/projects/${p.id}`}>{p.name}</Link></div></td>
                      <td><Avatar user={p.owner} size="sm" /></td>
                      <td><HealthBadge health={p.health} /></td>
                      <td style={{ minWidth: 80 }}><Progress value={p.metrics.rate_percent} color={p.color} /><p className="tiny muted" style={{ marginTop: 5 }}>{p.metrics.rate_percent}%</p></td>
                      <td>{fmtDate(p.due_on)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
          <Card title="Delivery momentum" sub={`Actual task completions vs task deadlines, ending ${fmtDate(s.as_of)}`}>
            <LineChart points={s.trend} asOf={s.as_of} />
          </Card>
          <Card title="Department focus">
            <div className="table-wrap">
              <table>
                <thead><tr><th>Department</th><th>Open work</th><th>Blocked</th><th>Overdue</th><th>Completion</th></tr></thead>
                <tbody>
                  {s.departments.map((d) => (
                    <tr key={d.name}><td>{d.name}</td><td>{d.metrics.open}</td><td>{d.metrics.blocked}</td><td>{d.metrics.overdue}</td><td>{d.metrics.rate_percent}%</td></tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
        </div>
        <div>
          <Card title="Decisions that need you">
            {s.intervention_queue.length ? (
              s.intervention_queue.map((t) => (
                <div className="risk-row" key={t.id}>
                  <span className="metric-icon orange"><Icon name="warning" /></span>
                  <div>
                    <button className="task-title" style={{ whiteSpace: "normal" }} onClick={() => openTask(t.id)}>{t.title}</button>
                    <p>{t.project.name}</p>
                    <div className="row" style={{ marginTop: 10 }}>
                      <StatusBadge status={t.status} />
                      <span className="tiny muted">{t.owner?.display_name.split(" ")[0]} · {fmtDate(t.due_on)}{t.overdue ? " · Late" : ""}</span>
                    </div>
                  </div>
                </div>
              ))
            ) : (
              <Empty title="Clear for the moment" desc="No overdue or blocked company tasks." />
            )}
          </Card>
          <section className="card intelligence-card">
            <div className="card-head"><h2 className="row"><Icon name="sparkles" /> Your executive lens</h2></div>
            <p className="intelligence-intro">{m.blocked} blocked tasks and {m.overdue} overdue tasks. Check the intervention queue before your next portfolio review.</p>
            <p className="tiny muted" style={{ marginTop: 12 }}>Rules-based insight · AI briefings arrive with the hybrid AI gateway</p>
          </section>
          <Card title="How this view works">
            {Object.entries(s.definitions).map(([k, v]) => (
              <p key={k} style={{ marginBottom: 8 }}><b className="tiny">{k.replace("_", " ").toUpperCase()}</b> · {v}</p>
            ))}
            <p className="tiny muted">Generated {new Date(s.generated_at).toLocaleString("en-GB")}</p>
          </Card>
        </div>
      </div>
    </>
  );
}
