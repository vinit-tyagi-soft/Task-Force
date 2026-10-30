"use client";

import { useParams } from "next/navigation";
import { ProjectsView } from "@/components/ProjectsView";
import { useSession } from "@/components/session";

export default function WorkspaceProjects() {
  const { workspaceId } = useParams<{ workspaceId: string }>();
  const { me } = useSession();
  return <ProjectsView key={workspaceId} workspaceId={workspaceId} personal={workspaceId === me.personal_workspace_id} />;
}
