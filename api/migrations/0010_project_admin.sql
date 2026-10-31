-- Project managers maintain milestones and project membership (RLS already scopes both to visible workspaces).
GRANT DELETE ON milestones, project_memberships TO taskforce_app;
