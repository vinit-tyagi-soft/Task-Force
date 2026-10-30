"use client";

import { useMemo, useState } from "react";
import { useApi } from "@/lib/api";
import { PRIORITIES, priorityLabel, STATUSES, statusLabel } from "@/lib/format";
import type { List, Metrics, Task } from "@/lib/types";
import { Icon } from "@/components/Icon";
import { Button, Heading, LoadError, Loading, Tabs } from "@/components/ui";
import { TaskTable } from "@/components/TaskTable";
import { useSession } from "@/components/session";

type View = "all" | "today" | "overdue" | "done";

export default function MyWork() {
  const { activeOrg, refreshKey, openTaskForm } = useSession();
  const work = useApi<List<Task> & { metrics: Metrics; today: string }>(`/me/work-items${activeOrg ? `?org=${activeOrg.id}` : ""}`, refreshKey);
  const [view, setView] = useState<View>("all");
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("");
  const [priority, setPriority] = useState("");
  const [scope, setScope] = useState("");
  const [sort, setSort] = useState("due");

  const tasks = useMemo(() => {
    const items = work.data?.items ?? [];
    const today = work.data?.today ?? "";
    const q = query.trim().toLowerCase();
    let ts = items.filter(
      (t) =>
        (!q || t.title.toLowerCase().includes(q) || t.project.name.toLowerCase().includes(q)) &&
        (!status || t.status === status) &&
        (!priority || t.priority === priority) &&
        (!scope || (scope === "PERSONAL") === (t.workspace_kind === "PERSONAL")),
    );
    if (view === "today") ts = ts.filter((t) => t.due_on === today);
    if (view === "overdue") ts = ts.filter((t) => t.overdue);
    if (view === "done") ts = ts.filter((t) => t.status === "DONE");
    const sorted = [...ts];
    if (sort === "priority") sorted.sort((a, b) => PRIORITIES.indexOf(a.priority) - PRIORITIES.indexOf(b.priority));
    else if (sort === "title") sorted.sort((a, b) => a.title.localeCompare(b.title));
    else sorted.sort((a, b) => (a.due_on ?? "9999").localeCompare(b.due_on ?? "9999"));
    return sorted;
  }, [work.data, view, query, status, priority, scope, sort]);

  if (work.error) return <LoadError message={work.error.message} onRetry={work.reload} />;
  if (!work.data) return <Loading />;

  return (
    <>
      <Heading title="My work" sub="One thoughtful list for everything you’re moving forward." actions={<Button icon="plus" variant="primary" onClick={() => openTaskForm()}>Create task</Button>} />
      <Tabs<View>
        items={[["all", "Assigned to me", work.data.items.length], ["today", "Due today"], ["overdue", "Overdue", work.data.metrics.overdue], ["done", "Completed"]]}
        active={view}
        onChange={setView}
      />
      <div className="toolbar">
        <div className="filter-group">
          <div className="filter-search">
            <Icon name="search" />
            <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search tasks…" aria-label="Filter tasks" />
          </div>
          <select value={status} onChange={(e) => setStatus(e.target.value)} aria-label="Filter by status">
            <option value="">All statuses</option>
            {STATUSES.map((s) => <option key={s} value={s}>{statusLabel[s]}</option>)}
          </select>
          <select value={priority} onChange={(e) => setPriority(e.target.value)} aria-label="Filter by priority">
            <option value="">All priorities</option>
            {PRIORITIES.map((p) => <option key={p} value={p}>{priorityLabel[p]}</option>)}
          </select>
          <select value={scope} onChange={(e) => setScope(e.target.value)} aria-label="Filter by workspace">
            <option value="">All workspaces</option>
            <option value="ORG">{activeOrg?.name ?? "Organization"}</option>
            <option value="PERSONAL">Private</option>
          </select>
        </div>
        <div className="filter-group">
          <select value={sort} onChange={(e) => setSort(e.target.value)} aria-label="Sort tasks">
            <option value="due">Due date</option>
            <option value="priority">Priority</option>
            <option value="title">Title A–Z</option>
          </select>
        </div>
      </div>
      <section className="card"><TaskTable tasks={tasks} /></section>
      <div className="task-footer">
        <span>{tasks.length} task{tasks.length === 1 ? "" : "s"} · {activeOrg?.name ?? "No organization"} + your private work</span>
        <span>Cancelled tasks are excluded from completion metrics</span>
      </div>
    </>
  );
}
