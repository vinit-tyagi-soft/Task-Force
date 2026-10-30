"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { api } from "@/lib/api";
import { roleLabel } from "@/lib/format";
import type { Me } from "@/lib/types";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Avatar, IconButton, Loading } from "@/components/ui";
import { SessionContext, type Session, type TaskFormDefaults } from "@/components/session";
import { TaskDrawer } from "@/components/TaskDrawer";
import { TaskForm } from "@/components/TaskForm";
import { ProjectForm } from "@/components/ProjectForm";

const ORG_KEY = "tf-active-org";

interface NavItem {
  label: string;
  icon: string;
  href?: string;
  match?: (path: string) => boolean;
  permission?: string;
}

export default function AppLayout({ children }: { children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const [me, setMe] = useState<Me | null>(null);
  const [activeOrgId, setActiveOrgId] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [drawerTask, setDrawerTask] = useState<string | null>(null);
  const [taskForm, setTaskForm] = useState<{ taskId?: string; defaults?: TaskFormDefaults } | null>(null);
  const [projectForm, setProjectForm] = useState<{ workspaceId: string; personal: boolean; projectId?: string } | null>(null);
  const [switcher, setSwitcher] = useState(false);
  const [profile, setProfile] = useState(false);
  const [toastMsg, setToastMsg] = useState<{ text: string; kind: string } | null>(null);
  // The shell renders a loading state until /me resolves, so reading storage here cannot cause a hydration mismatch.
  const [theme, setTheme] = useState<"light" | "dark">(() =>
    typeof window !== "undefined" && localStorage.getItem("tf-theme") === "dark" ? "dark" : "light",
  );
  const [collapsed, setCollapsed] = useState(false);
  const [mobileOpenAt, setMobileOpenAt] = useState<string | null>(null);
  const mobileOpen = mobileOpenAt === pathname;

  useEffect(() => {
    api<Me>("/me")
      .then((m) => {
        setMe(m);
        const stored = localStorage.getItem(ORG_KEY);
        const org = m.organizations.find((o) => o.id === stored) ?? m.organizations[0];
        setActiveOrgId(org?.id ?? null);
      })
      .catch(() => router.replace("/login"));
  }, [router]);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    document.body.classList.toggle("sidebar-collapsed", collapsed);
    document.body.classList.toggle("mobile-open", mobileOpen);
  }, [theme, collapsed, mobileOpen]);

  useEffect(() => {
    if (!toastMsg) return;
    const id = setTimeout(() => setToastMsg(null), 5000);
    return () => clearTimeout(id);
  }, [toastMsg]);

  const activeOrg = me?.organizations.find((o) => o.id === activeOrgId) ?? null;

  const setActiveOrg = useCallback(
    (id: string) => {
      if (!me) return;
      const org = me.organizations.find((o) => o.id === id);
      if (!org) return;
      localStorage.setItem(ORG_KEY, id);
      setActiveOrgId(id);
      setDrawerTask(null);
      if (pathname.startsWith("/workspaces/") || pathname.startsWith("/projects/") || pathname.startsWith("/board")) {
        router.push(`/workspaces/${org.workspace_id}/projects`);
      } else if (pathname.startsWith("/org/")) {
        router.push(`/org/${org.id}/executive`);
      }
    },
    [me, pathname, router],
  );

  const session: Session | null = useMemo(() => {
    if (!me) return null;
    return {
      me,
      activeOrg,
      setActiveOrg,
      refreshKey,
      refresh: () => setRefreshKey((k) => k + 1),
      openTask: setDrawerTask,
      openTaskForm: (opts) => setTaskForm(opts ?? {}),
      openProjectForm: setProjectForm,
      toast: (text, kind = "success") => setToastMsg({ text, kind }),
      can: (p) => !!activeOrg?.permissions.includes(p),
    };
  }, [me, activeOrg, setActiveOrg, refreshKey]);

  if (!me || !session) return <Loading />;

  const orgWs = activeOrg?.workspace_id;
  const nav: { group: string; items: NavItem[] }[] = [
    {
      group: "WORKSPACE",
      items: [
        { label: "Personal Dashboard", icon: "dashboard", href: "/personal", match: (p) => p === "/personal" },
        { label: "My Work", icon: "checkSquare", href: "/my-work" },
        { label: "My Calendar", icon: "calendar", href: "/calendar" },
        { label: "Personal Projects", icon: "lock", href: "/personal/projects" },
      ],
    },
    {
      group: "PLAN & DELIVER",
      items: [
        { label: "Projects & Tasks", icon: "folder", href: orgWs ? `/workspaces/${orgWs}/projects` : undefined, match: (p) => p.startsWith("/workspaces/") || (p.startsWith("/projects/") && !p.endsWith("/board")) },
        { label: "Kanban Boards", icon: "columns", href: "/board", match: (p) => p.startsWith("/board") || p.endsWith("/board") },
        { label: "Teams & Workload", icon: "users", href: "/teams" },
        { label: "Development & Hiring", icon: "growth" },
      ],
    },
    {
      group: "INSIGHTS",
      items: [
        { label: "Executive Dashboard", icon: "chart", href: activeOrg ? `/org/${activeOrg.id}/executive` : undefined, permission: "report.read", match: (p) => p.startsWith("/org/") },
        { label: "Reports & Insights", icon: "pie", href: "/reports", permission: "report.read" },
        { label: "AI Intelligence Center", icon: "sparkles" },
      ],
    },
    {
      group: "CONNECT",
      items: [
        { label: "Communication Hub", icon: "mail", href: "/messages" },
        { label: "Automations & Alerts", icon: "zap", href: "/automations" },
        { label: "API & Integrations", icon: "grid" },
      ],
    },
    { group: "MANAGE", items: [{ label: "Settings & Administration", icon: "settings", href: "/settings" }] },
  ];
  const allItems = nav.flatMap((g) => g.items);
  const current = allItems.find((i) => i.href && (i.match ? i.match(pathname) : pathname === i.href));

  async function signOut() {
    await api("/auth/logout", { method: "POST" }).catch(() => undefined);
    router.replace("/login");
  }

  const roles = activeOrg?.roles.map((r) => roleLabel[r] ?? r).join(", ") || "Personal only";

  return (
    <SessionContext.Provider value={session}>
      <a href="#main-content" className="skip">Skip to content</a>
      <div id="app-shell">
        <aside id="sidebar" aria-label="Main navigation">
          <Link className="brand" href="/personal" aria-label="TaskForce home">
            <span className="brandmark">
              <svg viewBox="0 0 24 28"><path d="m14 1-11 15h8l-2 11L22 11h-9z" /></svg>
            </span>
            <span className="brandtext">TaskForce<span className="version">V3</span></span>
          </Link>
          <button className="workspace" onClick={() => setSwitcher(true)}>
            <span className="workspace-icon">{activeOrg?.short_name ?? <Icon name="lock" />}</span>
            <span className="workspace-detail ellipsis">
              <span className="workspace-name">{activeOrg?.name ?? "Personal workspace"}</span>
              <p>{activeOrg ? "Enterprise workspace" : "Private"}</p>
            </span>
            <span className="spacer" />
            <Icon name="chevron" className="sm" />
          </button>
          <nav className="nav-scroll">
            {nav.map((g) => (
              <div className="navgroup" key={g.group}>
                <div className="navlabel">{g.group}</div>
                {g.items.map((item) => {
                  const allowed = !item.permission || session.can(item.permission);
                  const active = item === current;
                  if (!item.href || !allowed) {
                    return (
                      <span key={item.label} className="navitem navlocked" title={item.href ? "Requires a different role" : "Planned for a later release"} aria-disabled="true">
                        <Icon name={item.icon} />
                        <span className="navtext">{item.label}</span>
                        <span className="navcount" style={{ fontSize: 7 }}>{item.href ? "ROLE" : "SOON"}</span>
                      </span>
                    );
                  }
                  return (
                    <Link key={item.label} href={item.href} className={`navitem ${active ? "active" : ""}`} aria-current={active ? "page" : undefined} title={item.label}>
                      <Icon name={item.icon} />
                      <span className="navtext">{item.label}</span>
                    </Link>
                  );
                })}
              </div>
            ))}
          </nav>
          <div className="sidebar-bottom">
            <div className="storage-preview">
              <div className="row">
                <span className="live-dot" />
                <b className="tiny" style={{ fontWeight: 550 }}>Connected to TaskForce API</b>
              </div>
              <p>Server-side authorization and audit are active.</p>
            </div>
            <button className="user-button" onClick={() => setProfile(true)}>
              <Avatar user={me.user} />
              <span className="user-detail">
                <b style={{ fontSize: 10, fontWeight: 600 }}>{me.user.display_name}</b>
                <p className="tiny muted" style={{ fontSize: 8 }}>{roles}</p>
              </span>
              <span className="spacer" />
              <span className="user-detail"><Icon name="chevron" className="sm" /></span>
            </button>
          </div>
        </aside>
        <div className="app-body">
          <header id="topbar">
            <div className="row">
              <IconButton label="Open navigation" icon="menu" className="mobile-only" onClick={() => setMobileOpenAt(mobileOpen ? null : pathname)} />
              <IconButton label="Collapse or expand navigation" icon="panel" className="desktop-only" onClick={() => setCollapsed((v) => !v)} />
              <div className="breadcrumbs">
                <span>{activeOrg && !pathname.startsWith("/personal") && pathname !== "/my-work" ? activeOrg.name : "Workspace"}</span>
                <span className="muted">/</span>
                <b>{current?.label ?? "TaskForce"}</b>
              </div>
            </div>
            <div className="top-tools">
              <IconButton
                label={`Toggle ${theme === "light" ? "dark" : "light"} theme`}
                icon={theme === "light" ? "moon" : "sun"}
                onClick={() => {
                  const next = theme === "light" ? "dark" : "light";
                  localStorage.setItem("tf-theme", next);
                  setTheme(next);
                }}
              />
              <button className="btn primary" onClick={() => setTaskForm({})}>
                <Icon name="plus" className="sm" />
                Quick task
              </button>
              <Avatar user={me.user} />
            </div>
          </header>
          <main id="main-content" tabIndex={-1}>
            {children}
          </main>
          <footer id="footer">
            <span>TaskForce · Plan smarter. Execute faster. Lead better.</span>
            <span>Today {me.today} · {activeOrg?.name ?? "Personal"}</span>
          </footer>
        </div>
      </div>

      {switcher && (
        <Modal title="A place for every kind of work." onClose={() => setSwitcher(false)}>
          <p style={{ marginBottom: 18 }}>Choose an organization. Your private work always stays separate.</p>
          {me.organizations.map((o) => (
            <button key={o.id} className={`workspace-choice ${o.id === activeOrgId ? "active" : ""}`} onClick={() => { setActiveOrg(o.id); setSwitcher(false); session.toast(`Switched to ${o.name}`); }}>
              <span className="workspace-icon">{o.short_name}</span>
              <span>
                <h3>{o.name}</h3>
                <p className="tiny muted">{o.roles.map((r) => roleLabel[r] ?? r).join(", ")}</p>
              </span>
              <span className="spacer" />
              <Icon name={o.id === activeOrgId ? "check" : "arrowUp"} />
            </button>
          ))}
          <button className="workspace-choice" onClick={() => { setSwitcher(false); router.push("/personal/projects"); }}>
            <span className="workspace-icon"><Icon name="lock" /></span>
            <span>
              <h3>Your personal workspace</h3>
              <p className="tiny muted">Private projects, goals & notes</p>
            </span>
            <span className="spacer" />
            <Icon name="arrowUp" />
          </button>
        </Modal>
      )}

      {profile && (
        <Modal title="Your account" onClose={() => setProfile(false)}>
          <div className="row" style={{ marginBottom: 22 }}>
            <Avatar user={me.user} size="lg" />
            <div>
              <h3>{me.user.display_name}</h3>
              <p>{me.user.title} · {me.user.email}</p>
              <p className="tiny muted">Timezone {me.user.timezone}</p>
            </div>
          </div>
          {me.organizations.map((o) => (
            <div className="settings-row" key={o.id}>
              <h3>{o.name}</h3>
              <span className="tiny muted">{o.roles.map((r) => roleLabel[r] ?? r).join(", ")}</span>
            </div>
          ))}
          <div className="modal-actions">
            <button className="btn" onClick={signOut}><Icon name="logout" className="sm" />Sign out</button>
          </div>
        </Modal>
      )}

      {taskForm && <TaskForm taskId={taskForm.taskId} defaults={taskForm.defaults} onClose={() => setTaskForm(null)} />}
      {projectForm && <ProjectForm {...projectForm} onClose={() => setProjectForm(null)} />}
      {drawerTask && <TaskDrawer taskId={drawerTask} onClose={() => setDrawerTask(null)} />}

      <div id="toast-root" role="status" aria-live="polite">
        {toastMsg && (
          <div className="toast">
            <Icon name={toastMsg.kind === "warning" ? "warning" : "check"} />
            <span>{toastMsg.text}</span>
            <button onClick={() => setToastMsg(null)} aria-label="Dismiss notification" style={{ textDecoration: "none" }}>×</button>
          </div>
        )}
      </div>
    </SessionContext.Provider>
  );
}
