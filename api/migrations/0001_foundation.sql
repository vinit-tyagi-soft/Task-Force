-- TaskForce foundation schema: identity, tenancy, workspaces, projects, tasks, audit.
-- Every customer-data table carries workspace_id and is protected by row-level security.
-- The application connects as taskforce_app, which owns nothing and cannot bypass RLS.

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'taskforce_app') THEN
    CREATE ROLE taskforce_app NOLOGIN NOSUPERUSER NOBYPASSRLS;
  END IF;
END $$;

-- ---------------------------------------------------------------------------
-- Identity
-- ---------------------------------------------------------------------------
CREATE TABLE users (
  id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email                 text NOT NULL CHECK (length(email) BETWEEN 3 AND 320),
  display_name          text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 120),
  initials              text NOT NULL CHECK (length(initials) BETWEEN 1 AND 3),
  title                 text NOT NULL DEFAULT '',
  avatar_color          text NOT NULL DEFAULT '#7964da' CHECK (avatar_color ~ '^#[0-9a-fA-F]{6}$'),
  timezone              text NOT NULL DEFAULT 'UTC',
  weekly_capacity_hours integer NOT NULL DEFAULT 40 CHECK (weekly_capacity_hours BETWEEN 0 AND 168),
  state                 text NOT NULL DEFAULT 'ACTIVE' CHECK (state IN ('ACTIVE', 'SUSPENDED', 'DEACTIVATED')),
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

CREATE TABLE identity_links (
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  issuer     text NOT NULL,
  subject    text NOT NULL,
  provider   text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (issuer, subject)
);

-- ---------------------------------------------------------------------------
-- Tenancy
-- ---------------------------------------------------------------------------
CREATE TABLE organizations (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  slug                text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
  name                text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  short_name          text NOT NULL CHECK (length(short_name) BETWEEN 1 AND 4),
  region              text NOT NULL,
  reporting_timezone  text NOT NULL DEFAULT 'UTC',
  accent_color        text NOT NULL DEFAULT '#7660dc' CHECK (accent_color ~ '^#[0-9a-fA-F]{6}$'),
  status              text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'SUSPENDED')),
  created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE org_memberships (
  org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  state      text NOT NULL DEFAULT 'ACTIVE' CHECK (state IN ('INVITED', 'ACTIVE', 'SUSPENDED', 'REMOVED')),
  joined_at  timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, user_id)
);
CREATE INDEX org_memberships_user_idx ON org_memberships (user_id);

CREATE TABLE departments (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  head_user_id uuid REFERENCES users (id),
  created_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, name),
  UNIQUE (id, tenant_id)
);

CREATE TABLE role_bindings (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  uuid NOT NULL,
  user_id    uuid NOT NULL,
  role_key   text NOT NULL CHECK (role_key IN (
               'TENANT_OWNER', 'ORG_ADMIN', 'EXECUTIVE', 'DEPARTMENT_HEAD', 'PROJECT_MANAGER',
               'TEAM_LEAD', 'MEMBER', 'VIEWER', 'HR_SPECIALIST')),
  scope_type text NOT NULL DEFAULT 'ORG' CHECK (scope_type IN ('ORG', 'DEPARTMENT', 'PROJECT', 'TEAM')),
  scope_id   uuid,
  created_by uuid REFERENCES users (id),
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (tenant_id, user_id) REFERENCES org_memberships (org_id, user_id) ON DELETE CASCADE,
  CHECK ((scope_type = 'ORG') = (scope_id IS NULL))
);
CREATE UNIQUE INDEX role_bindings_unique ON role_bindings
  (tenant_id, user_id, role_key, scope_type, coalesce(scope_id, '00000000-0000-0000-0000-000000000000'));

-- ---------------------------------------------------------------------------
-- Workspaces: PERSONAL (one owner, no tenant) xor ORG (one tenant, no owner)
-- ---------------------------------------------------------------------------
CREATE TABLE workspaces (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  kind          text NOT NULL CHECK (kind IN ('PERSONAL', 'ORG')),
  tenant_id     uuid REFERENCES organizations (id) ON DELETE CASCADE,
  owner_user_id uuid REFERENCES users (id) ON DELETE CASCADE,
  name          text NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  CHECK (
    (kind = 'PERSONAL' AND owner_user_id IS NOT NULL AND tenant_id IS NULL) OR
    (kind = 'ORG' AND tenant_id IS NOT NULL AND owner_user_id IS NULL)
  )
);
CREATE UNIQUE INDEX workspaces_one_personal ON workspaces (owner_user_id) WHERE kind = 'PERSONAL';
CREATE UNIQUE INDEX workspaces_one_org ON workspaces (tenant_id) WHERE kind = 'ORG';

-- ---------------------------------------------------------------------------
-- Projects and tasks
-- ---------------------------------------------------------------------------
CREATE TABLE projects (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id  uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  tenant_id     uuid REFERENCES organizations (id) ON DELETE CASCADE,
  department_id uuid,
  name          text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
  description   text NOT NULL DEFAULT '' CHECK (length(description) <= 1000),
  owner_id      uuid NOT NULL REFERENCES users (id),
  state         text NOT NULL DEFAULT 'ACTIVE' CHECK (state IN ('ACTIVE', 'ARCHIVED')),
  start_on      date NOT NULL,
  due_on        date NOT NULL,
  color         text NOT NULL DEFAULT '#7964da' CHECK (color ~ '^#[0-9a-fA-F]{6}$'),
  icon          text NOT NULL DEFAULT 'folder',
  version       integer NOT NULL DEFAULT 1,
  created_by    uuid NOT NULL REFERENCES users (id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  deleted_at    timestamptz,
  CHECK (due_on >= start_on),
  UNIQUE (id, workspace_id),
  FOREIGN KEY (department_id, tenant_id) REFERENCES departments (id, tenant_id)
);
CREATE INDEX projects_workspace_idx ON projects (workspace_id, state) WHERE deleted_at IS NULL;
CREATE INDEX projects_tenant_idx ON projects (tenant_id, state) WHERE deleted_at IS NULL;

CREATE TABLE project_memberships (
  project_id   uuid NOT NULL,
  workspace_id uuid NOT NULL,
  user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  access_level text NOT NULL DEFAULT 'CONTRIBUTOR' CHECK (access_level IN ('MANAGER', 'CONTRIBUTOR', 'VIEWER')),
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (project_id, user_id),
  FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE
);
CREATE INDEX project_memberships_user_idx ON project_memberships (user_id);

CREATE TABLE milestones (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id   uuid NOT NULL,
  workspace_id uuid NOT NULL,
  title        text NOT NULL CHECK (length(title) BETWEEN 1 AND 100),
  target_on    date NOT NULL,
  state        text NOT NULL DEFAULT 'OPEN' CHECK (state IN ('OPEN', 'DONE')),
  created_at   timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE
);
CREATE INDEX milestones_project_idx ON milestones (project_id, target_on);

CREATE TABLE tasks (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  number           bigint GENERATED BY DEFAULT AS IDENTITY (START WITH 101) UNIQUE,
  workspace_id     uuid NOT NULL,
  tenant_id        uuid REFERENCES organizations (id) ON DELETE CASCADE,
  project_id       uuid NOT NULL,
  parent_task_id   uuid REFERENCES tasks (id),
  title            text NOT NULL CHECK (length(title) BETWEEN 1 AND 180),
  description      text NOT NULL DEFAULT '' CHECK (length(description) <= 3000),
  status           text NOT NULL DEFAULT 'TODO' CHECK (status IN ('TODO', 'IN_PROGRESS', 'IN_REVIEW', 'BLOCKED', 'DONE', 'CANCELLED')),
  priority         text NOT NULL DEFAULT 'MEDIUM' CHECK (priority IN ('URGENT', 'HIGH', 'MEDIUM', 'LOW')),
  owner_id         uuid NOT NULL REFERENCES users (id),
  creator_id       uuid NOT NULL REFERENCES users (id),
  start_on         date,
  due_on           date,
  estimate_minutes integer NOT NULL DEFAULT 0 CHECK (estimate_minutes BETWEEN 0 AND 30000),
  blocked_reason   text NOT NULL DEFAULT '' CHECK (length(blocked_reason) <= 1000),
  completed_at     timestamptz,
  version          integer NOT NULL DEFAULT 1,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  deleted_at       timestamptz,
  FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE,
  CHECK (due_on IS NULL OR start_on IS NULL OR due_on >= start_on),
  CHECK (status <> 'BLOCKED' OR length(blocked_reason) > 0),
  CHECK ((status = 'DONE') = (completed_at IS NOT NULL))
);
CREATE INDEX tasks_tenant_project_idx ON tasks (tenant_id, project_id, status, due_on) WHERE deleted_at IS NULL;
CREATE INDEX tasks_owner_idx ON tasks (workspace_id, owner_id, due_on) WHERE deleted_at IS NULL;
CREATE INDEX tasks_owner_global_idx ON tasks (owner_id, due_on) WHERE deleted_at IS NULL;

CREATE TABLE task_comments (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id      uuid NOT NULL REFERENCES tasks (id) ON DELETE CASCADE,
  workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
  author_id    uuid NOT NULL REFERENCES users (id),
  body         text NOT NULL CHECK (length(body) BETWEEN 1 AND 1500),
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX task_comments_task_idx ON task_comments (task_id, created_at);

-- ---------------------------------------------------------------------------
-- Audit (append-only) and idempotency
-- ---------------------------------------------------------------------------
CREATE TABLE audit_events (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    uuid REFERENCES organizations (id),
  workspace_id uuid REFERENCES workspaces (id),
  actor_id     uuid NOT NULL REFERENCES users (id),
  action       text NOT NULL,
  target_type  text NOT NULL,
  target_id    uuid,
  summary      text NOT NULL DEFAULT '',
  metadata     jsonb NOT NULL DEFAULT '{}'::jsonb,
  request_id   text NOT NULL DEFAULT '',
  occurred_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_tenant_idx ON audit_events (tenant_id, occurred_at DESC);
CREATE INDEX audit_events_actor_idx ON audit_events (actor_id, occurred_at DESC);

CREATE TABLE idempotency_keys (
  actor_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  key           text NOT NULL CHECK (length(key) BETWEEN 8 AND 128),
  request_hash  text NOT NULL,
  resource_type text NOT NULL,
  resource_id   uuid NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (actor_id, key)
);

-- ---------------------------------------------------------------------------
-- Request context helpers. app.user_id / app.tenant_id are set per transaction
-- (set_config(..., true)), so they never leak across pooled connections.
-- ---------------------------------------------------------------------------
CREATE FUNCTION tf_current_user() RETURNS uuid
  LANGUAGE sql STABLE AS $$ SELECT nullif(current_setting('app.user_id', true), '')::uuid $$;

CREATE FUNCTION tf_current_tenant() RETURNS uuid
  LANGUAGE sql STABLE AS $$ SELECT nullif(current_setting('app.tenant_id', true), '')::uuid $$;

CREATE FUNCTION tf_is_member(p_org uuid) RETURNS boolean
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT EXISTS (
    SELECT 1 FROM org_memberships m JOIN organizations o ON o.id = m.org_id
    WHERE m.org_id = p_org AND m.user_id = tf_current_user()
      AND m.state = 'ACTIVE' AND o.status = 'ACTIVE')
$$;

-- A tenant row is visible only to active members, and only inside the active
-- tenant context when one is set (journey D: switching tenant narrows results).
CREATE FUNCTION tf_tenant_visible(p_org uuid) RETURNS boolean
  LANGUAGE sql STABLE AS $$
  SELECT p_org IS NOT NULL AND tf_is_member(p_org)
     AND (tf_current_tenant() IS NULL OR tf_current_tenant() = p_org)
$$;

CREATE FUNCTION tf_workspace_visible(p_workspace uuid) RETURNS boolean
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT EXISTS (
    SELECT 1 FROM workspaces w
    WHERE w.id = p_workspace
      AND ((w.kind = 'PERSONAL' AND w.owner_user_id = tf_current_user())
        OR (w.kind = 'ORG' AND tf_tenant_visible(w.tenant_id))))
$$;

CREATE FUNCTION tf_shares_org(p_user uuid) RETURNS boolean
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT EXISTS (
    SELECT 1 FROM org_memberships mine JOIN org_memberships theirs ON theirs.org_id = mine.org_id
    WHERE mine.user_id = tf_current_user() AND mine.state = 'ACTIVE'
      AND theirs.user_id = p_user AND theirs.state IN ('ACTIVE', 'INVITED', 'SUSPENDED'))
$$;

-- Login must resolve an identity before a user context exists.
CREATE FUNCTION tf_auth_lookup_user(p_email text) RETURNS TABLE (id uuid, state text)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT u.id, u.state FROM users u WHERE lower(u.email) = lower(p_email)
$$;

-- ---------------------------------------------------------------------------
-- Derive tenant/workspace from the parent so children can never cross scopes.
-- ---------------------------------------------------------------------------
CREATE FUNCTION tf_project_scope() RETURNS trigger
  LANGUAGE plpgsql SECURITY DEFINER SET search_path = public AS $$
DECLARE ws workspaces%ROWTYPE;
BEGIN
  SELECT * INTO ws FROM workspaces WHERE id = NEW.workspace_id;
  IF NOT FOUND THEN RAISE EXCEPTION 'workspace not found' USING ERRCODE = '23503'; END IF;
  NEW.tenant_id := ws.tenant_id;
  IF ws.kind = 'PERSONAL' AND NEW.owner_id <> ws.owner_user_id THEN
    RAISE EXCEPTION 'personal projects must be owned by the workspace owner' USING ERRCODE = '23514';
  END IF;
  IF ws.kind = 'PERSONAL' AND NEW.department_id IS NOT NULL THEN
    RAISE EXCEPTION 'personal projects cannot belong to a department' USING ERRCODE = '23514';
  END IF;
  IF ws.kind = 'ORG' AND NOT EXISTS (
      SELECT 1 FROM org_memberships WHERE org_id = ws.tenant_id AND user_id = NEW.owner_id AND state = 'ACTIVE') THEN
    RAISE EXCEPTION 'project owner must be an active organization member' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER projects_scope BEFORE INSERT OR UPDATE ON projects
  FOR EACH ROW EXECUTE FUNCTION tf_project_scope();

CREATE FUNCTION tf_project_member_scope() RETURNS trigger
  LANGUAGE plpgsql SECURITY DEFINER SET search_path = public AS $$
DECLARE ws workspaces%ROWTYPE;
BEGIN
  SELECT w.* INTO ws FROM workspaces w WHERE w.id = NEW.workspace_id;
  IF ws.kind = 'PERSONAL' AND NEW.user_id <> ws.owner_user_id THEN
    RAISE EXCEPTION 'personal projects cannot be shared' USING ERRCODE = '23514';
  END IF;
  IF ws.kind = 'ORG' AND NOT EXISTS (
      SELECT 1 FROM org_memberships WHERE org_id = ws.tenant_id AND user_id = NEW.user_id AND state = 'ACTIVE') THEN
    RAISE EXCEPTION 'project member must be an active organization member' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER project_memberships_scope BEFORE INSERT OR UPDATE ON project_memberships
  FOR EACH ROW EXECUTE FUNCTION tf_project_member_scope();

CREATE FUNCTION tf_task_scope() RETURNS trigger
  LANGUAGE plpgsql SECURITY DEFINER SET search_path = public AS $$
DECLARE ws workspaces%ROWTYPE;
BEGIN
  SELECT w.* INTO ws FROM projects p JOIN workspaces w ON w.id = p.workspace_id WHERE p.id = NEW.project_id;
  IF NOT FOUND THEN RAISE EXCEPTION 'project not found' USING ERRCODE = '23503'; END IF;
  NEW.workspace_id := ws.id;
  NEW.tenant_id := ws.tenant_id;
  IF ws.kind = 'PERSONAL' AND NEW.owner_id <> ws.owner_user_id THEN
    RAISE EXCEPTION 'private personal tasks can only be owned by the workspace owner' USING ERRCODE = '23514';
  END IF;
  IF ws.kind = 'ORG' AND NOT EXISTS (
      SELECT 1 FROM org_memberships WHERE org_id = ws.tenant_id AND user_id = NEW.owner_id AND state = 'ACTIVE') THEN
    RAISE EXCEPTION 'task owner must be an active organization member' USING ERRCODE = '23514';
  END IF;
  IF NEW.parent_task_id IS NOT NULL AND NOT EXISTS (
      SELECT 1 FROM tasks WHERE id = NEW.parent_task_id AND project_id = NEW.project_id) THEN
    RAISE EXCEPTION 'parent task must belong to the same project' USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER tasks_scope BEFORE INSERT OR UPDATE ON tasks
  FOR EACH ROW EXECUTE FUNCTION tf_task_scope();

CREATE FUNCTION tf_child_of_task_scope() RETURNS trigger
  LANGUAGE plpgsql SECURITY DEFINER SET search_path = public AS $$
BEGIN
  SELECT workspace_id INTO NEW.workspace_id FROM tasks WHERE id = NEW.task_id;
  RETURN NEW;
END $$;
CREATE TRIGGER task_comments_scope BEFORE INSERT ON task_comments
  FOR EACH ROW EXECUTE FUNCTION tf_child_of_task_scope();

CREATE FUNCTION tf_audit_immutable() RETURNS trigger
  LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit events are append-only' USING ERRCODE = '42501';
END $$;
CREATE TRIGGER audit_events_immutable BEFORE UPDATE OR DELETE ON audit_events
  FOR EACH ROW EXECUTE FUNCTION tf_audit_immutable();

-- ---------------------------------------------------------------------------
-- Row-level security (defense in depth; the Go policy layer still authorizes
-- every action against role bindings and project access).
-- ---------------------------------------------------------------------------
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;
CREATE POLICY users_visible ON users FOR SELECT
  USING (id = tf_current_user() OR tf_shares_org(id));

ALTER TABLE identity_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE identity_links FORCE ROW LEVEL SECURITY;
CREATE POLICY identity_links_self ON identity_links FOR SELECT USING (user_id = tf_current_user());

ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organizations FORCE ROW LEVEL SECURITY;
CREATE POLICY organizations_member ON organizations FOR SELECT USING (tf_is_member(id));

ALTER TABLE org_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_memberships FORCE ROW LEVEL SECURITY;
CREATE POLICY org_memberships_visible ON org_memberships FOR SELECT
  USING (user_id = tf_current_user() OR tf_tenant_visible(org_id));

ALTER TABLE role_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_bindings FORCE ROW LEVEL SECURITY;
CREATE POLICY role_bindings_visible ON role_bindings FOR SELECT
  USING (user_id = tf_current_user() OR tf_tenant_visible(tenant_id));

ALTER TABLE departments ENABLE ROW LEVEL SECURITY;
ALTER TABLE departments FORCE ROW LEVEL SECURITY;
CREATE POLICY departments_visible ON departments FOR SELECT USING (tf_tenant_visible(tenant_id));

ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspaces FORCE ROW LEVEL SECURITY;
CREATE POLICY workspaces_visible ON workspaces FOR SELECT USING (tf_workspace_visible(id));

ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE projects FORCE ROW LEVEL SECURITY;
CREATE POLICY projects_scope ON projects FOR ALL
  USING (tf_workspace_visible(workspace_id)) WITH CHECK (tf_workspace_visible(workspace_id));

ALTER TABLE project_memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_memberships FORCE ROW LEVEL SECURITY;
CREATE POLICY project_memberships_scope ON project_memberships FOR ALL
  USING (tf_workspace_visible(workspace_id)) WITH CHECK (tf_workspace_visible(workspace_id));

ALTER TABLE milestones ENABLE ROW LEVEL SECURITY;
ALTER TABLE milestones FORCE ROW LEVEL SECURITY;
CREATE POLICY milestones_scope ON milestones FOR ALL
  USING (tf_workspace_visible(workspace_id)) WITH CHECK (tf_workspace_visible(workspace_id));

ALTER TABLE tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY tasks_scope ON tasks FOR ALL
  USING (tf_workspace_visible(workspace_id)) WITH CHECK (tf_workspace_visible(workspace_id));

ALTER TABLE task_comments ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_comments FORCE ROW LEVEL SECURITY;
CREATE POLICY task_comments_scope ON task_comments FOR ALL
  USING (tf_workspace_visible(workspace_id))
  WITH CHECK (tf_workspace_visible(workspace_id) AND author_id = tf_current_user());

ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY audit_events_read ON audit_events FOR SELECT
  USING (actor_id = tf_current_user() OR tf_tenant_visible(tenant_id));
CREATE POLICY audit_events_write ON audit_events FOR INSERT
  WITH CHECK (actor_id = tf_current_user() AND (tenant_id IS NULL OR tf_is_member(tenant_id)));

ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY idempotency_keys_self ON idempotency_keys FOR ALL
  USING (actor_id = tf_current_user()) WITH CHECK (actor_id = tf_current_user());

-- ---------------------------------------------------------------------------
-- Grants for the application role (no DDL, no audit mutation).
-- ---------------------------------------------------------------------------
GRANT USAGE ON SCHEMA public TO taskforce_app;
GRANT SELECT ON users, identity_links, organizations, org_memberships, role_bindings, departments, workspaces TO taskforce_app;
GRANT SELECT, INSERT, UPDATE ON projects, project_memberships, milestones, tasks TO taskforce_app;
GRANT SELECT, INSERT ON task_comments, audit_events TO taskforce_app;
GRANT SELECT, INSERT, DELETE ON idempotency_keys TO taskforce_app;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO taskforce_app;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO taskforce_app;
