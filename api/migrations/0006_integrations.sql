-- API & Integrations: hashed, org-scoped API tokens and signed outbound webhooks.

CREATE TABLE api_tokens (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    uuid NOT NULL,
  user_id      uuid NOT NULL,
  name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  token_hash   text NOT NULL UNIQUE,
  prefix       text NOT NULL,
  scope        text NOT NULL CHECK (scope IN ('read', 'write')),
  expires_at   timestamptz NOT NULL,
  last_used_at timestamptz,
  revoked_at   timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (tenant_id, user_id) REFERENCES org_memberships (org_id, user_id) ON DELETE CASCADE
);
CREATE INDEX api_tokens_tenant_idx ON api_tokens (tenant_id, created_at DESC);

CREATE TABLE webhook_endpoints (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  name       text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  url        text NOT NULL CHECK (url ~ '^https://' AND length(url) <= 500),
  events     text[] NOT NULL CHECK (cardinality(events) BETWEEN 1 AND 20),
  secret     text NOT NULL,
  enabled    boolean NOT NULL DEFAULT true,
  version    integer NOT NULL DEFAULT 1,
  created_by uuid NOT NULL REFERENCES users (id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, tenant_id)
);

CREATE TABLE webhook_deliveries (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       uuid NOT NULL,
  endpoint_id     uuid NOT NULL,
  event           text NOT NULL,
  payload         jsonb NOT NULL,
  status          text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'DELIVERED', 'FAILED')),
  attempts        integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  response_code   integer,
  error           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  delivered_at    timestamptz,
  FOREIGN KEY (endpoint_id, tenant_id) REFERENCES webhook_endpoints (id, tenant_id) ON DELETE CASCADE
);
CREATE INDEX webhook_deliveries_due_idx ON webhook_deliveries (next_attempt_at) WHERE status = 'PENDING';
CREATE INDEX webhook_deliveries_endpoint_idx ON webhook_deliveries (tenant_id, created_at DESC);

ALTER TABLE api_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY api_tokens_read ON api_tokens FOR SELECT USING (tf_tenant_visible(tenant_id));
CREATE POLICY api_tokens_create ON api_tokens FOR INSERT
  WITH CHECK (tf_tenant_visible(tenant_id) AND user_id = tf_current_user());
CREATE POLICY api_tokens_revoke ON api_tokens FOR UPDATE
  USING (tf_tenant_visible(tenant_id)) WITH CHECK (tf_tenant_visible(tenant_id));

ALTER TABLE webhook_endpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_endpoints FORCE ROW LEVEL SECURITY;
CREATE POLICY webhook_endpoints_scope ON webhook_endpoints FOR ALL
  USING (tf_tenant_visible(tenant_id)) WITH CHECK (tf_tenant_visible(tenant_id));

ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries FORCE ROW LEVEL SECURITY;
CREATE POLICY webhook_deliveries_read ON webhook_deliveries FOR SELECT USING (tf_tenant_visible(tenant_id));
CREATE POLICY webhook_deliveries_enqueue ON webhook_deliveries FOR INSERT WITH CHECK (tf_tenant_visible(tenant_id));

-- Token authentication runs before any user context exists, so it resolves the
-- hash through a definer function that returns only the binding, never the hash.
CREATE FUNCTION tf_auth_use_token(p_hash text) RETURNS TABLE (user_id uuid, tenant_id uuid, scope text)
  LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public AS $$
  UPDATE api_tokens SET last_used_at = now()
  WHERE token_hash = p_hash AND revoked_at IS NULL AND expires_at > now()
  RETURNING api_tokens.user_id, api_tokens.tenant_id, api_tokens.scope
$$;

-- The delivery worker has no user context; it leases due deliveries of enabled
-- endpoints (SKIP LOCKED, so several API instances can run it) and records results.
CREATE FUNCTION tf_webhook_claim(p_limit integer)
  RETURNS TABLE (id uuid, url text, secret text, event text, payload jsonb, attempts integer)
  LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public AS $$
  WITH due AS (
    SELECT d.id FROM webhook_deliveries d JOIN webhook_endpoints e ON e.id = d.endpoint_id
    WHERE d.status = 'PENDING' AND d.next_attempt_at <= now() AND e.enabled
    ORDER BY d.next_attempt_at LIMIT p_limit FOR UPDATE OF d SKIP LOCKED
  )
  UPDATE webhook_deliveries d SET attempts = d.attempts + 1, next_attempt_at = now() + interval '5 minutes'
  FROM due, webhook_endpoints e
  WHERE d.id = due.id AND e.id = d.endpoint_id
  RETURNING d.id, e.url, e.secret, d.event, d.payload, d.attempts
$$;

CREATE FUNCTION tf_webhook_complete(p_id uuid, p_ok boolean, p_code integer, p_error text) RETURNS void
  LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public AS $$
  UPDATE webhook_deliveries SET
    status = CASE WHEN p_ok THEN 'DELIVERED' WHEN attempts >= 5 THEN 'FAILED' ELSE 'PENDING' END,
    response_code = p_code,
    error = left(p_error, 300),
    delivered_at = CASE WHEN p_ok THEN now() END,
    next_attempt_at = now() + make_interval(secs => 30 * power(2, attempts))
  WHERE id = p_id
$$;

REVOKE EXECUTE ON FUNCTION tf_auth_use_token(text), tf_webhook_claim(integer), tf_webhook_complete(uuid, boolean, integer, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION tf_auth_use_token(text), tf_webhook_claim(integer), tf_webhook_complete(uuid, boolean, integer, text) TO taskforce_app;

GRANT SELECT, INSERT, UPDATE ON api_tokens TO taskforce_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON webhook_endpoints TO taskforce_app;
GRANT SELECT, INSERT ON webhook_deliveries TO taskforce_app;
