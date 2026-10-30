-- AI Intelligence Center: each person's question history, private to them.
CREATE TABLE ai_queries (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    uuid NOT NULL,
  user_id      uuid NOT NULL,
  question     text NOT NULL CHECK (length(question) BETWEEN 1 AND 300),
  intent       text NOT NULL,
  source_count integer NOT NULL DEFAULT 0,
  created_at   timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (tenant_id, user_id) REFERENCES org_memberships (org_id, user_id) ON DELETE CASCADE
);
CREATE INDEX ai_queries_user_idx ON ai_queries (user_id, tenant_id, created_at DESC);

ALTER TABLE ai_queries ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_queries FORCE ROW LEVEL SECURITY;
CREATE POLICY ai_queries_own ON ai_queries FOR ALL
  USING (user_id = tf_current_user() AND tf_tenant_visible(tenant_id))
  WITH CHECK (user_id = tf_current_user() AND tf_tenant_visible(tenant_id));

GRANT SELECT, INSERT ON ai_queries TO taskforce_app;
