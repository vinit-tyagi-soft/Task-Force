-- Teams & Workload: org-scoped teams whose members must be members of the same org.

CREATE TABLE teams (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  name          text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  description   text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
  department_id uuid,
  lead_user_id  uuid,
  color         text NOT NULL DEFAULT '#7964da' CHECK (color ~ '^#[0-9a-fA-F]{6}$'),
  version       integer NOT NULL DEFAULT 1,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, tenant_id),
  FOREIGN KEY (department_id, tenant_id) REFERENCES departments (id, tenant_id),
  FOREIGN KEY (tenant_id, lead_user_id) REFERENCES org_memberships (org_id, user_id)
);
CREATE UNIQUE INDEX teams_name_key ON teams (tenant_id, lower(name));

CREATE TABLE team_members (
  team_id    uuid NOT NULL,
  tenant_id  uuid NOT NULL,
  user_id    uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (team_id, user_id),
  FOREIGN KEY (team_id, tenant_id) REFERENCES teams (id, tenant_id) ON DELETE CASCADE,
  FOREIGN KEY (tenant_id, user_id) REFERENCES org_memberships (org_id, user_id) ON DELETE CASCADE
);
CREATE INDEX team_members_user_idx ON team_members (user_id);

ALTER TABLE teams ENABLE ROW LEVEL SECURITY;
ALTER TABLE teams FORCE ROW LEVEL SECURITY;
CREATE POLICY teams_scope ON teams FOR ALL
  USING (tf_tenant_visible(tenant_id)) WITH CHECK (tf_tenant_visible(tenant_id));

ALTER TABLE team_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE team_members FORCE ROW LEVEL SECURITY;
CREATE POLICY team_members_scope ON team_members FOR ALL
  USING (tf_tenant_visible(tenant_id)) WITH CHECK (tf_tenant_visible(tenant_id));

GRANT SELECT, INSERT, UPDATE, DELETE ON teams, team_members TO taskforce_app;
