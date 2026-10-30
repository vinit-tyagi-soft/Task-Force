-- Automations & Alerts: org rules evaluated on demand, run history and per-user notifications.

CREATE TABLE automation_rules (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id   uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  trigger     text NOT NULL CHECK (trigger IN ('TASK_OVERDUE', 'TASK_BLOCKED', 'TASK_DUE_SOON')),
  action      text NOT NULL CHECK (action IN ('NOTIFY_TASK_OWNER', 'NOTIFY_PROJECT_OWNER', 'NOTIFY_PROJECT_MANAGERS')),
  enabled     boolean NOT NULL DEFAULT true,
  last_run_at timestamptz,
  version     integer NOT NULL DEFAULT 1,
  created_by  uuid NOT NULL REFERENCES users (id),
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, tenant_id)
);

CREATE TABLE automation_runs (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  uuid NOT NULL,
  rule_id    uuid NOT NULL,
  rule_name  text NOT NULL,
  started_by uuid NOT NULL REFERENCES users (id),
  matched    integer NOT NULL CHECK (matched >= 0),
  notified   integer NOT NULL CHECK (notified >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (rule_id, tenant_id) REFERENCES automation_rules (id, tenant_id) ON DELETE CASCADE
);
CREATE INDEX automation_runs_tenant_idx ON automation_runs (tenant_id, created_at DESC);

CREATE TABLE notifications (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  uuid NOT NULL,
  user_id    uuid NOT NULL,
  rule_id    uuid,
  task_id    uuid REFERENCES tasks (id) ON DELETE CASCADE,
  title      text NOT NULL CHECK (length(title) BETWEEN 1 AND 300),
  detail     text NOT NULL DEFAULT '',
  dedupe_on  date NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  read_at    timestamptz,
  FOREIGN KEY (tenant_id, user_id) REFERENCES org_memberships (org_id, user_id) ON DELETE CASCADE,
  FOREIGN KEY (rule_id, tenant_id) REFERENCES automation_rules (id, tenant_id) ON DELETE SET NULL (rule_id)
);
CREATE UNIQUE INDEX notifications_dedupe_key ON notifications (rule_id, task_id, user_id, dedupe_on);
CREATE INDEX notifications_user_idx ON notifications (user_id, tenant_id, created_at DESC);

ALTER TABLE automation_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE automation_rules FORCE ROW LEVEL SECURITY;
CREATE POLICY automation_rules_scope ON automation_rules FOR ALL
  USING (tf_tenant_visible(tenant_id)) WITH CHECK (tf_tenant_visible(tenant_id));

ALTER TABLE automation_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE automation_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY automation_runs_read ON automation_runs FOR SELECT USING (tf_tenant_visible(tenant_id));
CREATE POLICY automation_runs_write ON automation_runs FOR INSERT
  WITH CHECK (tf_tenant_visible(tenant_id) AND started_by = tf_current_user());

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
CREATE POLICY notifications_own ON notifications FOR SELECT
  USING (user_id = tf_current_user() AND tf_tenant_visible(tenant_id));
CREATE POLICY notifications_own_update ON notifications FOR UPDATE
  USING (user_id = tf_current_user() AND tf_tenant_visible(tenant_id))
  WITH CHECK (user_id = tf_current_user() AND tf_tenant_visible(tenant_id));
CREATE POLICY notifications_create ON notifications FOR INSERT WITH CHECK (tf_tenant_visible(tenant_id));

GRANT SELECT, INSERT, UPDATE, DELETE ON automation_rules TO taskforce_app;
GRANT SELECT, INSERT ON automation_runs TO taskforce_app;
GRANT SELECT, INSERT, UPDATE ON notifications TO taskforce_app;
