"use client";

import { ProjectsView } from "@/components/ProjectsView";
import { useSession } from "@/components/session";
import { Empty } from "@/components/ui";

export default function PersonalProjects() {
  const { me } = useSession();
  if (!me.personal_workspace_id) return <Empty title="No personal workspace" desc="Your account does not have a personal workspace yet." />;
  return <ProjectsView workspaceId={me.personal_workspace_id} personal />;
}
