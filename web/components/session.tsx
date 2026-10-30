"use client";

import { createContext, useContext } from "react";
import type { Me, Organization, Status } from "@/lib/types";

export interface TaskFormDefaults {
  projectId?: string;
  status?: Status;
  dueOn?: string;
}

export interface Session {
  me: Me;
  activeOrg: Organization | null;
  setActiveOrg: (id: string) => void;
  /** Bumped after every successful mutation so views refetch server state. */
  refreshKey: number;
  refresh: () => void;
  openTask: (id: string) => void;
  openTaskForm: (opts?: { taskId?: string; defaults?: TaskFormDefaults }) => void;
  openProjectForm: (opts: { workspaceId: string; personal: boolean; projectId?: string }) => void;
  toast: (text: string, kind?: "success" | "warning") => void;
  can: (permission: string) => boolean;
}

export const SessionContext = createContext<Session | null>(null);

export function useSession(): Session {
  const s = useContext(SessionContext);
  if (!s) throw new Error("useSession must be used inside the app shell");
  return s;
}
