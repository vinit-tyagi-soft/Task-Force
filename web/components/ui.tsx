"use client";

import type { ReactNode } from "react";
import { Icon } from "./Icon";
import { healthLabel, priorityLabel, statusLabel, toneFor } from "@/lib/format";
import type { Health, Priority, Project, Status, User } from "@/lib/types";

export function Avatar({ user, size = "" }: { user: User | null | undefined; size?: "" | "sm" | "lg" }) {
  if (!user) return null;
  return (
    <span className={`avatar ${size}`} style={{ ["--av" as string]: user.avatar_color }} title={user.display_name}>
      {user.initials}
    </span>
  );
}

export function Avatars({ users }: { users: User[] }) {
  return (
    <div className="avatar-stack">
      {users.slice(0, 4).map((u) => (
        <Avatar key={u.id} user={u} size="sm" />
      ))}
      {users.length > 4 && (
        <span className="avatar sm" style={{ ["--av" as string]: "#b9b3cf" }}>
          +{users.length - 4}
        </span>
      )}
    </div>
  );
}

export function Badge({ label, tone }: { label: string; tone?: string }) {
  return (
    <span className={`badge ${tone ?? toneFor(label)}`}>
      <span className="dot" />
      {label}
    </span>
  );
}

export const StatusBadge = ({ status }: { status: Status }) => <Badge label={statusLabel[status]} />;
export const HealthBadge = ({ health }: { health: Health }) => <Badge label={healthLabel[health]} />;

export function PriorityTag({ priority }: { priority: Priority }) {
  const label = priorityLabel[priority];
  return (
    <span className={`priority ${label}`}>
      <Icon name="flag" className="sm" />
      {label}
    </span>
  );
}

export function Progress({ value, color = "var(--primary)" }: { value: number; color?: string }) {
  return (
    <div className="progress-track" role="progressbar" aria-valuenow={value} aria-valuemin={0} aria-valuemax={100} aria-label="Completion">
      <span style={{ width: `${value}%`, background: color }} />
    </div>
  );
}

export function ProjectIcon({ project }: { project: Pick<Project, "color" | "icon"> }) {
  return (
    <span className="project-icon" style={{ ["--project" as string]: project.color, ["--project-bg" as string]: `${project.color}13` }}>
      <Icon name={project.icon} />
    </span>
  );
}

export function Card({ title, sub, extra, className = "", children }: { title?: ReactNode; sub?: ReactNode; extra?: ReactNode; className?: string; children: ReactNode }) {
  return (
    <section className={`card ${className}`}>
      <div className="card-head">
        <div>
          {title && <h2>{title}</h2>}
          {sub && <p>{sub}</p>}
        </div>
        {extra}
      </div>
      {children}
    </section>
  );
}

export function Heading({ title, sub, actions }: { title: string; sub: ReactNode; actions?: ReactNode }) {
  return (
    <div className="page-heading">
      <div>
        <h1>{title}</h1>
        <p>{sub}</p>
      </div>
      <div className="row actions">{actions}</div>
    </div>
  );
}

export function Empty({ title, desc, action }: { title: string; desc: string; action?: ReactNode }) {
  return (
    <div className="empty">
      <Icon name="folder" />
      <h3>{title}</h3>
      <p>{desc}</p>
      {action}
    </div>
  );
}

export function Notice({ children, tone = "", icon = "info" }: { children: ReactNode; tone?: string; icon?: string }) {
  return (
    <div className={`notice ${tone}`}>
      <Icon name={icon} />
      <span>{children}</span>
    </div>
  );
}

export function Tabs<T extends string>({ items, active, onChange }: { items: [T, string, number?][]; active: T; onChange: (id: T) => void }) {
  return (
    <div className="tabs page-tabs" role="tablist">
      {items.map(([id, label, count]) => (
        <button key={id} role="tab" aria-selected={id === active} className={`tab ${id === active ? "active" : ""}`} onClick={() => onChange(id)}>
          {label}
          {count !== undefined && <span className="count">{count}</span>}
        </button>
      ))}
    </div>
  );
}

export function Metric({ label, value, sub, icon = "checkSquare", tone = "", onClick }: { label: string; value: ReactNode; sub: ReactNode; icon?: string; tone?: string; onClick?: () => void }) {
  const body = (
    <>
      <div className="row between">
        <span className="metric-label">{label}</span>
        <span className={`metric-icon ${tone}`}>
          <Icon name={icon} />
        </span>
      </div>
      <div className="metric-value">{value}</div>
      <div className="metric-sub">{sub}</div>
    </>
  );
  return onClick ? (
    <button className="metric clickable" onClick={onClick}>
      {body}
    </button>
  ) : (
    <div className="metric">{body}</div>
  );
}

export function Button({ children, icon, variant = "", onClick, type = "button", disabled }: { children: ReactNode; icon?: string; variant?: string; onClick?: () => void; type?: "button" | "submit"; disabled?: boolean }) {
  return (
    <button type={type} className={`btn ${variant}`} onClick={onClick} disabled={disabled}>
      {icon && <Icon name={icon} className="sm" />}
      {children}
    </button>
  );
}

export function IconButton({ label, icon, onClick, className = "" }: { label: string; icon: string; onClick?: () => void; className?: string }) {
  return (
    <button className={`iconbtn ${className}`} title={label} aria-label={label} onClick={onClick}>
      <Icon name={icon} />
    </button>
  );
}

export function Loading() {
  return <div className="empty" aria-busy="true"><p>Loading…</p></div>;
}

export function LoadError({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return <Empty title="We couldn’t load this view" desc={message} action={onRetry && <Button onClick={onRetry} icon="refresh" variant="soft">Try again</Button>} />;
}
