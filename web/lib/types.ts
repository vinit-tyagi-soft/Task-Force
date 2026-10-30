export type Status = "TODO" | "IN_PROGRESS" | "IN_REVIEW" | "BLOCKED" | "DONE" | "CANCELLED";
export type Priority = "URGENT" | "HIGH" | "MEDIUM" | "LOW";
export type Health = "ON_TRACK" | "NEEDS_ATTENTION" | "AT_RISK";
export type Access = "NONE" | "READ" | "CONTRIBUTE" | "MANAGE";

export interface User {
  id: string;
  display_name: string;
  initials: string;
  title: string;
  avatar_color: string;
}

export interface Organization {
  id: string;
  slug: string;
  name: string;
  short_name: string;
  region: string;
  reporting_timezone: string;
  accent_color: string;
  workspace_id: string;
  roles: string[];
  permissions: string[];
}

export interface Me {
  user: User & { email: string; timezone: string };
  personal_workspace_id: string | null;
  today: string;
  organizations: Organization[];
}

export interface Metrics {
  eligible: number;
  done: number;
  open: number;
  blocked: number;
  overdue: number;
  attention: number;
  rate_percent: number;
  open_hours: number;
}

export interface Project {
  id: string;
  workspace_id: string;
  workspace_kind: "PERSONAL" | "ORG";
  tenant_id: string | null;
  name: string;
  description: string;
  department_id: string | null;
  department: string;
  owner: User | null;
  state: "ACTIVE" | "ARCHIVED";
  start_on: string;
  due_on: string;
  color: string;
  icon: string;
  version: number;
  access: Access;
  metrics: Metrics;
  health: Health;
  member_ids: string[];
  members: User[];
  updated_at: string;
}

export interface Milestone {
  id: string;
  title: string;
  target_on: string;
  state: "OPEN" | "DONE";
}

export interface Task {
  id: string;
  number: number;
  key: string;
  project: { id: string; name: string; color: string };
  workspace_kind: "PERSONAL" | "ORG";
  tenant_id: string | null;
  title: string;
  description: string;
  status: Status;
  priority: Priority;
  owner: User | null;
  start_on: string | null;
  due_on: string | null;
  estimate_hours: number;
  blocked_reason: string;
  completed_at: string | null;
  version: number;
  overdue: boolean;
  comment_count: number;
  can_edit: boolean;
  created_at: string;
  updated_at: string;
}

export interface Comment {
  id: string;
  author: User | null;
  body: string;
  created_at: string;
}

export interface AuditEvent {
  id: string;
  actor: User | null;
  action: string;
  target_type: string;
  target_id: string | null;
  summary: string;
  request_id: string;
  occurred_at: string;
}

export interface ExecutiveSummary {
  organization: { id: string; name: string; timezone: string };
  generated_at: string;
  as_of: string;
  metrics: Metrics;
  projects_at_risk: number;
  projects: Project[];
  departments: { name: string; metrics: Metrics }[];
  intervention_queue: Task[];
  trend: { date: string; planned: number; completed: number }[];
  definitions: Record<string, string>;
}

export interface List<T> {
  items: T[];
}
