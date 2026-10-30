"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { useApi } from "@/lib/api";
import { addDays, fmtDate } from "@/lib/format";
import type { List, Metrics, Project, Task } from "@/lib/types";
import { Icon } from "@/components/Icon";
import { Badge, Button, Card, HealthBadge, Heading, LoadError, Loading, Metric, Progress, ProjectIcon, Tabs } from "@/components/ui";
import { Donut, LineChart } from "@/components/charts";
import { TaskTable } from "@/components/TaskTable";
import { useSession } from "@/components/session";

type Tab = "today" | "upcoming" | "all" | "completed";

export default function PersonalDashboard() {
  const { me, activeOrg, refreshKey, openTaskForm, openTask } = useSession();
  const [tab, setTab] = useState<Tab>("today");
  const work = useApi<List<Task> & { metrics: Metrics; today: string }>(`/me/work-items${activeOrg ? `?org=${activeOrg.id}` : ""}`, refreshKey);
  const projects = useApi<List<Project>>(activeOrg ? `/workspaces/${activeOrg.workspace_id}/projects` : null, refreshKey);

  const view = useMemo(() => {
    const items = work.data?.items ?? [];
    const today = work.data?.today ?? me.today;
    const terminal = (t: Task) => t.status === "DONE" || t.status === "CANCELLED";
    const due = items.filter((t) => t.due_on === today && !terminal(t));
    const next = items.filter((t) => t.due_on && t.due_on > today && !terminal(t));
    const done = items.filter((t) => t.status === "DONE");
    const trend = Array.from({ length: 7 }, (_, i) => {
      const d = addDays(today, i - 6);
      return {
        date: d,
        planned: items.filter((t) => t.due_on === d && t.status !== "CANCELLED").length,
        completed: items.filter((t) => t.completed_at && t.completed_at.slice(0, 10) === d).length,
      };
    });
    return { items, today, due, next, done, trend };
  }, [work.data, me.today]);

  if (work.error) return <LoadError message={work.error.message} onRetry={work.reload} />;
  if (!work.data) return <Loading />;

  const m = work.data.metrics;
  const shown = { today: view.due, upcoming: view.next, all: view.items, completed: view.done }[tab];
  const orgProjects = projects.data?.items ?? [];
  const blocked = orgProjects.reduce((n, p) => n + p.metrics.blocked, 0);
  const firstName = me.user.display_name.split(" ")[0];

  return (
    <>
      <Heading
        title="A little clarity. A lot of possibility."
        sub={`${new Date(`${view.today}T12:00:00`).toLocaleDateString("en", { weekday: "long" })} focus, ${firstName}. Let’s make space for what matters.`}
        actions={
          <>
            <span className="row date-button"><Icon name="calendar" className="sm" />{fmtDate(view.today, { weekday: "short", year: "numeric" })}</span>
            <Button icon="plus" variant="primary" onClick={() => openTaskForm()}>Create task</Button>
          </>
        }
      />
      <section className="welcome-banner">
        <div>
          <div className="eyebrow">YOUR WORK, BEAUTIFULLY IN SYNC</div>
          <h2>Big plans. Thoughtful progress.</h2>
          <p>You have {view.due.length} priorities due today. One clear next step at a time.</p>
        </div>
        <div className="banner-stat">
          <b style={{ fontSize: 22, fontWeight: 500 }}>{m.rate_percent}%</b>
          <br />of your tasks completed
        </div>
      </section>
      <div className="metrics">
        <Metric label="My open tasks" value={m.open} sub={`Across ${activeOrg?.name ?? "your organization"} & private work`} />
        <Metric label="Due today" value={view.due.length} sub="Keep your next steps in focus" icon="calendar" tone="orange" />
        <Metric label="Completed tasks" value={m.done} sub={<><span className="trend">{m.rate_percent}%</span> overall completion</>} tone="green" />
        <Metric label="Needs attention" value={m.attention} sub="Blocked or overdue (counted once)" icon="warning" tone="blue" />
      </div>
      <div className="dashboard-grid">
        <div>
          <section className="card">
            <div className="card-head">
              <h2>Your priorities</h2>
              <Link className="textbtn" href="/my-work">View all tasks <Icon name="arrowUp" className="sm" /></Link>
            </div>
            <Tabs<Tab>
              items={[["today", "Today", view.due.length], ["upcoming", "Upcoming", view.next.length], ["all", "All work", view.items.length], ["completed", "Completed", view.done.length]]}
              active={tab}
              onChange={setTab}
            />
            <TaskTable tasks={shown.slice(0, 5)} compact />
            <div className="task-footer">
              <span>{shown.length ? `Showing ${Math.min(5, shown.length)} of ${shown.length} tasks` : "Everything is up to date"}</span>
              <button className="textbtn" onClick={() => openTaskForm()}>+ Add a task</button>
            </div>
          </section>
          <div className="chart-row">
            <Card title="A week of steady progress" sub="Completions and task deadlines from your records" extra={<Badge label="Your work" tone="gray" />}>
              <LineChart points={view.trend} asOf={view.today} />
            </Card>
            <Card title="Work at a glance">
              <Donut statuses={view.items.map((t) => t.status)} />
            </Card>
          </div>
        </div>
        <div className="dashboard-right">
          <section className="card intelligence-card">
            <div className="card-head">
              <h2 className="row"><Icon name="sparkles" className="sm" /> A clearer perspective</h2>
              <Badge label="Rules-based" tone="gray" />
            </div>
            <p className="intelligence-intro">A little context for a more considered day.</p>
            <div className="insight-item">
              <span className="insight-symbol"><Icon name="target" /></span>
              <div>
                <h3>{blocked} dependencies need attention</h3>
                <p>{blocked ? "Review blocked work in your authorized projects before the next milestone." : "No blocked tasks in the projects you can see."}</p>
              </div>
            </div>
            <div className="insight-item">
              <span className="insight-symbol"><Icon name="clock" /></span>
              <div>
                <h3>Make a little room for focus</h3>
                <p>{view.due.length} priorities land today. Start with your highest-impact work and protect time to finish.</p>
              </div>
            </div>
            <div className="tiny muted" style={{ marginTop: 12, fontSize: 8 }}>Deterministic rules · No AI model connected yet</div>
          </section>
          <Card title="Project pulse" extra={activeOrg && <Link className="textbtn" href={`/workspaces/${activeOrg.workspace_id}/projects`}>View all <Icon name="arrowUp" className="sm" /></Link>}>
            {orgProjects.slice(0, 3).map((p) => (
              <div className="project-pulse" key={p.id}>
                <div className="row between">
                  <div className="row">
                    <ProjectIcon project={p} />
                    <div>
                      <Link className="task-title" style={{ fontSize: 10 }} href={`/projects/${p.id}`}>{p.name}</Link>
                      <p>{p.department} · Due {fmtDate(p.due_on)}</p>
                    </div>
                  </div>
                  <HealthBadge health={p.health} />
                </div>
                <div style={{ margin: "12px 0 7px" }}><Progress value={p.metrics.rate_percent} color={p.color} /></div>
                <div className="row between tiny muted">
                  <span>{p.metrics.done} of {p.metrics.eligible} tasks completed</span>
                  <span>{p.metrics.rate_percent}%</span>
                </div>
              </div>
            ))}
            {!orgProjects.length && <p>No organization projects yet.</p>}
          </Card>
          <Card title="Coming into view">
            {view.next.slice(0, 3).map((t) => (
              <div className="meeting" key={t.id}>
                <div className="meeting-time">{fmtDate(t.due_on)}</div>
                <div className="meeting-content">
                  <button className="task-title" style={{ fontSize: 10 }} onClick={() => openTask(t.id)}>{t.title}</button>
                  <p>{t.project.name}</p>
                </div>
              </div>
            ))}
            {!view.next.length && <p>No upcoming deadlines.</p>}
          </Card>
        </div>
      </div>
    </>
  );
}
