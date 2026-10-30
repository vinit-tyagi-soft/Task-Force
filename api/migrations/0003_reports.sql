-- Reports & Insights: private saved report views and an append-only export history.

CREATE TABLE report_views (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  uuid NOT NULL,
  owner_id   uuid NOT NULL,
  name       text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  filters    jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (tenant_id, owner_id) REFERENCES org_memberships (org_id, user_id) ON DELETE CASCADE
);
CREATE INDEX report_views_owner_idx ON report_views (tenant_id, owner_id);

CREATE TABLE report_exports (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  actor_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  name       text NOT NULL,
  row_count  integer NOT NULL CHECK (row_count >= 0),
  filters    jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX report_exports_actor_idx ON report_exports (tenant_id, actor_id, created_at DESC);

ALTER TABLE report_views ENABLE ROW LEVEL SECURITY;
ALTER TABLE report_views FORCE ROW LEVEL SECURITY;
CREATE POLICY report_views_owner ON report_views FOR ALL
  USING (owner_id = tf_current_user() AND tf_tenant_visible(tenant_id))
  WITH CHECK (owner_id = tf_current_user() AND tf_tenant_visible(tenant_id));

ALTER TABLE report_exports ENABLE ROW LEVEL SECURITY;
ALTER TABLE report_exports FORCE ROW LEVEL SECURITY;
CREATE POLICY report_exports_read ON report_exports FOR SELECT
  USING (actor_id = tf_current_user() AND tf_tenant_visible(tenant_id));
CREATE POLICY report_exports_write ON report_exports FOR INSERT
  WITH CHECK (actor_id = tf_current_user() AND tf_tenant_visible(tenant_id));

GRANT SELECT, INSERT, DELETE ON report_views TO taskforce_app;
GRANT SELECT, INSERT ON report_exports TO taskforce_app;
