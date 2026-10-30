"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useApi } from "@/lib/api";
import type { List, Project } from "@/lib/types";
import { Empty, Loading } from "@/components/ui";
import { useSession } from "@/components/session";

/** Opens the board of the first project in the active organization (or personal workspace). */
export default function BoardIndex() {
  const router = useRouter();
  const { me, activeOrg } = useSession();
  const ws = activeOrg?.workspace_id ?? me.personal_workspace_id;
  const projects = useApi<List<Project>>(ws ? `/workspaces/${ws}/projects` : null);
  const first = projects.data?.items[0];

  useEffect(() => {
    if (first) router.replace(`/projects/${first.id}/board`);
  }, [first, router]);

  if (projects.data && !first) return <Empty title="No boards yet" desc="Create or join a project to see its board." />;
  return <Loading />;
}
