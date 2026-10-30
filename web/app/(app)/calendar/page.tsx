"use client";

import { useState } from "react";
import { useApi } from "@/lib/api";
import { addDays, fmtDate } from "@/lib/format";
import type { List, Task } from "@/lib/types";
import { Badge, Button, Card, Empty, Heading, IconButton, LoadError, Loading, StatusBadge } from "@/components/ui";
import { useSession } from "@/components/session";

type Mode = "month" | "week" | "day";
type Scope = "mine" | "company" | "personal";
interface CalProject { id: string; name: string; color: string }
interface CalMilestone { id: string; title: string; target_on: string; state: string; project: CalProject }
type CalendarData = List<Task> & { today: string; milestones: CalMilestone[]; projects: CalProject[] };

const WEEKDAYS = ["MON", "TUE", "WED", "THU", "FRI", "SAT", "SUN"];

/** Monday-based day index (0 = Monday). */
const dow = (d: string) => (new Date(`${d}T12:00:00Z`).getUTCDay() + 6) % 7;

function monthStart(d: string, delta = 0): string {
  const x = new Date(`${d.slice(0, 7)}-01T12:00:00Z`);
  x.setUTCMonth(x.getUTCMonth() + delta);
  return x.toISOString().slice(0, 10);
}

function visibleDays(mode: Mode, selected: string): string[] {
  if (mode === "day") return [selected];
  if (mode === "week") {
    const start = addDays(selected, -dow(selected));
    return Array.from({ length: 7 }, (_, i) => addDays(start, i));
  }
  const first = monthStart(selected);
  const start = addDays(first, -dow(first));
  return Array.from({ length: 42 }, (_, i) => addDays(start, i));
}

export default function CalendarPage() {
  const { me, activeOrg, refreshKey, openTask, openTaskForm } = useSession();
  const [mode, setMode] = useState<Mode>("month");
  const [selected, setSelected] = useState(me.today);
  const [scope, setScope] = useState<Scope>("mine");

  const days = visibleDays(mode, selected);
  const effectiveScope: Scope = scope === "company" && !activeOrg ? "mine" : scope;
  const org = activeOrg && effectiveScope !== "personal" ? `&org=${activeOrg.id}` : "";
  const cal = useApi<CalendarData>(`/me/calendar?scope=${effectiveScope}&from=${days[0]}&to=${days[days.length - 1]}${org}`, refreshKey);

  const move = (n: number) =>
    setSelected(mode === "month" ? monthStart(selected, n) : addDays(selected, n * (mode === "week" ? 7 : 1)));
  const openDay = (d: string) => {
    setSelected(d);
    setMode("day");
  };

  const tasksOn = (d: string) => (cal.data?.items ?? []).filter((t) => t.due_on === d);
  const milestonesOn = (d: string) => (cal.data?.milestones ?? []).filter((m) => m.target_on === d);
  const agenda = tasksOn(selected);
  const agendaMilestones = milestonesOn(selected);
  const month = Number(selected.slice(5, 7));
  const title =
    mode === "month"
      ? new Date(`${monthStart(selected)}T12:00:00`).toLocaleDateString("en", { month: "long", year: "numeric" })
      : fmtDate(selected, { year: "numeric" });

  const planButton = (variant: string) => (
    <Button icon="plus" variant={variant} onClick={() => openTaskForm({ defaults: { dueOn: selected } })}>Plan a task</Button>
  );

  let grid;
  if (cal.error) grid = <LoadError message={cal.error.message} onRetry={cal.reload} />;
  else if (!cal.data) grid = <Loading />;
  else if (mode === "day")
    grid = (
      <div style={{ minHeight: 400 }}>
        <div className="eyebrow">{fmtDate(selected, { weekday: "long", year: "numeric" })}</div>
        {agendaMilestones.map((m) => (
          <div className="day-agenda" key={m.id}>
            <span className="tiny muted" style={{ width: 55 }}>Milestone</span>
            <div><strong>{m.title}</strong><p className="tiny muted">{m.project.name}</p></div>
            <span className="spacer" />
            <Badge label={m.state === "DONE" ? "Done" : "Planned"} />
          </div>
        ))}
        {agenda.map((t) => (
          <div className="day-agenda" key={t.id}>
            <span className="tiny muted" style={{ width: 55 }}>Due today</span>
            <div>
              <button className="task-title" onClick={() => openTask(t.id)}>{t.title}</button>
              <p className="tiny muted">{t.project.name}</p>
            </div>
            <span className="spacer" />
            <StatusBadge status={t.status} />
          </div>
        ))}
        {!agenda.length && !agendaMilestones.length && <Empty title="Your day has a little space" desc="No task deadlines on this date." action={planButton("soft")} />}
      </div>
    );
  else
    grid = (
      <div className="calendar-grid">
        {WEEKDAYS.map((d) => <div className="weekday" key={d}>{d}</div>)}
        {days.map((d) => {
          const ts = tasksOn(d);
          const ms = milestonesOn(d);
          const limit = mode === "week" ? 20 : 3;
          const hidden = ts.length + ms.length - limit;
          const cls = ["cal-cell", Number(d.slice(5, 7)) !== month && mode === "month" ? "other" : "", d === cal.data!.today ? "today" : "", mode === "week" ? "week-cell" : ""];
          return (
            <div key={d} className={cls.filter(Boolean).join(" ")}>
              <button className="cal-number" aria-label={`View ${d}`} onClick={() => openDay(d)}>{Number(d.slice(8))}</button>
              {ms.slice(0, limit).map((m) => (
                <button key={m.id} className="cal-event" style={{ ["--project" as string]: m.project.color, ["--project-bg" as string]: `${m.project.color}16`, fontWeight: 700 }} title={`Milestone: ${m.title}`} onClick={() => openDay(d)}>
                  ◆ {m.title}
                </button>
              ))}
              {ts.slice(0, Math.max(0, limit - ms.length)).map((t) => (
                <button key={t.id} className="cal-event" style={{ ["--project" as string]: t.project.color, ["--project-bg" as string]: `${t.project.color}16` }} title={t.title} onClick={() => openTask(t.id)}>
                  {t.title}
                </button>
              ))}
              {hidden > 0 && <button className="textbtn" style={{ fontSize: 7 }} onClick={() => openDay(d)}>+{hidden} more</button>}
            </div>
          );
        })}
      </div>
    );

  return (
    <>
      <Heading title="Your time, thoughtfully planned." sub="Deadlines, priorities, and a little room to do your best work." actions={planButton("primary")} />
      <div className="toolbar">
        <div className="row">
          <IconButton label="Previous period" icon="left" onClick={() => move(-1)} />
          <h2>{title}</h2>
          <IconButton label="Next period" icon="right" onClick={() => move(1)} />
          <Button onClick={() => setSelected(me.today)}>Today</Button>
        </div>
        <div className="row">
          <select aria-label="Calendar scope" value={effectiveScope} onChange={(e) => setScope(e.target.value as Scope)}>
            <option value="mine">My assigned work</option>
            <option value="company" disabled={!activeOrg}>Company work</option>
            <option value="personal">Private work</option>
          </select>
          <div className="segmented">
            {(["month", "week", "day"] as Mode[]).map((x) => (
              <button key={x} className={x === mode ? "active" : ""} onClick={() => setMode(x)}>{x[0].toUpperCase() + x.slice(1)}</button>
            ))}
          </div>
        </div>
      </div>
      <div className="calendar-layout">
        <section className="card">{grid}</section>
        <div className="stack">
          <Card title={`On ${fmtDate(selected)}`}>
            {agenda.length || agendaMilestones.length ? (
              <>
                {agendaMilestones.map((m) => (
                  <div className="meeting" key={m.id}><div className="meeting-content"><strong style={{ fontSize: 11 }}>◆ {m.title}</strong><p>{m.project.name}</p></div></div>
                ))}
                {agenda.map((t) => (
                  <div className="meeting" key={t.id}>
                    <div className="meeting-content">
                      <button className="task-title" style={{ fontSize: 11, whiteSpace: "normal" }} onClick={() => openTask(t.id)}>{t.title}</button>
                      <p>{t.project.name}</p>
                    </div>
                  </div>
                ))}
              </>
            ) : (
              <p>No task deadlines for this date.</p>
            )}
          </Card>
          <Card title="Your projects">
            {(cal.data?.projects ?? []).map((p) => (
              <div key={p.id} className="row" style={{ padding: "9px 0", fontSize: 10, color: "var(--soft)" }}>
                <span className="scope-dot" style={{ ["--project" as string]: p.color }} />
                {p.name}
              </div>
            ))}
          </Card>
        </div>
      </div>
    </>
  );
}
