-- Per-organization AI policy. No row means the defaults below.
CREATE TABLE ai_policies (
  tenant_id       uuid PRIMARY KEY REFERENCES organizations (id) ON DELETE CASCADE,
  enabled         boolean NOT NULL DEFAULT true,
  allowed_intents text[] NOT NULL DEFAULT ARRAY['BLOCKED', 'OVERDUE', 'PRIORITIES', 'WORKLOAD', 'SUMMARY'],
  retention_days  integer NOT NULL DEFAULT 90 CHECK (retention_days IN (7, 30, 90, 365)),
  version         integer NOT NULL DEFAULT 1,
  updated_by      uuid REFERENCES users (id),
  updated_at      timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE ai_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_policies FORCE ROW LEVEL SECURITY;
CREATE POLICY ai_policies_read ON ai_policies FOR SELECT USING (tf_tenant_visible(tenant_id));
CREATE POLICY ai_policies_insert ON ai_policies FOR INSERT
  WITH CHECK (tf_tenant_visible(tenant_id) AND tf_org_admin(tenant_id));
CREATE POLICY ai_policies_update ON ai_policies FOR UPDATE
  USING (tf_tenant_visible(tenant_id) AND tf_org_admin(tenant_id)) WITH CHECK (tf_tenant_visible(tenant_id) AND tf_org_admin(tenant_id));

GRANT SELECT, INSERT, UPDATE ON ai_policies TO taskforce_app;
GRANT DELETE ON ai_queries TO taskforce_app;
