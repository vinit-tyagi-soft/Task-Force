import type { Health, Priority, Status } from "./types";

export const STATUSES: Status[] = ["TODO", "IN_PROGRESS", "IN_REVIEW", "BLOCKED", "DONE", "CANCELLED"];
export const PRIORITIES: Priority[] = ["URGENT", "HIGH", "MEDIUM", "LOW"];

export const statusLabel: Record<Status, string> = {
  TODO: "To do",
  IN_PROGRESS: "In progress",
  IN_REVIEW: "In review",
  BLOCKED: "Blocked",
  DONE: "Done",
  CANCELLED: "Cancelled",
};

export const priorityLabel: Record<Priority, string> = { URGENT: "Urgent", HIGH: "High", MEDIUM: "Medium", LOW: "Low" };

export const healthLabel: Record<Health, string> = {
  ON_TRACK: "On track",
  NEEDS_ATTENTION: "Needs attention",
  AT_RISK: "At risk",
};

export const roleLabel: Record<string, string> = {
  TENANT_OWNER: "Tenant owner",
  ORG_ADMIN: "Org admin",
  EXECUTIVE: "Executive / PMO",
  DEPARTMENT_HEAD: "Department head",
  PROJECT_MANAGER: "Project manager",
  TEAM_LEAD: "Team lead",
  MEMBER: "Member",
  VIEWER: "Viewer / auditor",
  HR_SPECIALIST: "HR specialist",
};

const badgeTone: Record<string, string> = {
  Done: "green",
  "On track": "green",
  "In progress": "blue",
  "At risk": "red",
  Blocked: "red",
  Urgent: "red",
  High: "red",
  "Needs attention": "orange",
  "To do": "gray",
  Cancelled: "gray",
};

export function toneFor(label: string): string {
  return badgeTone[label] ?? "";
}

/** Formats a YYYY-MM-DD date or ISO timestamp like the prototype ("30 Oct"). */
export function fmtDate(value: string | null | undefined, opts: Intl.DateTimeFormatOptions = {}): string {
  if (!value) return "No date";
  const d = new Date(value.length === 10 ? `${value}T12:00:00` : value);
  return d.toLocaleDateString("en-GB", { day: "numeric", month: "short", ...opts });
}

export function addDays(date: string, n: number): string {
  const d = new Date(`${date}T12:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
}

export function weekday(date: string): string {
  return new Date(`${date}T12:00:00`).toLocaleDateString("en", { weekday: "short" });
}
