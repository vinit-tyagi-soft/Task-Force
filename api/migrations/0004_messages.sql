-- Communication Hub: org-internal threads visible only to their participants.

ALTER TABLE projects ADD CONSTRAINT projects_id_tenant_key UNIQUE (id, tenant_id);

CREATE TABLE message_threads (
  id              uuid PRIMARY KEY,
  tenant_id       uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  subject         text NOT NULL CHECK (length(subject) BETWEEN 1 AND 160),
  project_id      uuid,
  created_by      uuid NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  last_message_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, tenant_id),
  FOREIGN KEY (project_id, tenant_id) REFERENCES projects (id, tenant_id) ON DELETE SET NULL (project_id),
  FOREIGN KEY (tenant_id, created_by) REFERENCES org_memberships (org_id, user_id)
);
CREATE INDEX message_threads_tenant_idx ON message_threads (tenant_id, last_message_at DESC);

CREATE TABLE thread_participants (
  thread_id    uuid NOT NULL,
  tenant_id    uuid NOT NULL,
  user_id      uuid NOT NULL,
  last_read_at timestamptz,
  starred      boolean NOT NULL DEFAULT false,
  PRIMARY KEY (thread_id, user_id),
  FOREIGN KEY (thread_id, tenant_id) REFERENCES message_threads (id, tenant_id) ON DELETE CASCADE,
  FOREIGN KEY (tenant_id, user_id) REFERENCES org_memberships (org_id, user_id) ON DELETE CASCADE
);
CREATE INDEX thread_participants_user_idx ON thread_participants (user_id);

CREATE TABLE messages (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  thread_id  uuid NOT NULL,
  tenant_id  uuid NOT NULL,
  author_id  uuid NOT NULL REFERENCES users (id),
  body       text NOT NULL CHECK (length(body) BETWEEN 1 AND 10000),
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (thread_id, tenant_id) REFERENCES message_threads (id, tenant_id) ON DELETE CASCADE
);
CREATE INDEX messages_thread_idx ON messages (thread_id, created_at);

CREATE FUNCTION tf_thread_member(p_thread uuid) RETURNS boolean
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT EXISTS (SELECT 1 FROM thread_participants WHERE thread_id = p_thread AND user_id = tf_current_user())
$$;

CREATE FUNCTION tf_thread_creator(p_thread uuid) RETURNS boolean
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT EXISTS (SELECT 1 FROM message_threads WHERE id = p_thread AND created_by = tf_current_user())
$$;

ALTER TABLE message_threads ENABLE ROW LEVEL SECURITY;
ALTER TABLE message_threads FORCE ROW LEVEL SECURITY;
CREATE POLICY message_threads_read ON message_threads FOR SELECT
  USING (tf_tenant_visible(tenant_id) AND tf_thread_member(id));
CREATE POLICY message_threads_create ON message_threads FOR INSERT
  WITH CHECK (tf_tenant_visible(tenant_id) AND created_by = tf_current_user());
CREATE POLICY message_threads_touch ON message_threads FOR UPDATE
  USING (tf_tenant_visible(tenant_id) AND tf_thread_member(id));

ALTER TABLE thread_participants ENABLE ROW LEVEL SECURITY;
ALTER TABLE thread_participants FORCE ROW LEVEL SECURITY;
CREATE POLICY thread_participants_read ON thread_participants FOR SELECT
  USING (tf_tenant_visible(tenant_id) AND tf_thread_member(thread_id));
CREATE POLICY thread_participants_add ON thread_participants FOR INSERT
  WITH CHECK (tf_tenant_visible(tenant_id) AND tf_thread_creator(thread_id));
CREATE POLICY thread_participants_own_state ON thread_participants FOR UPDATE
  USING (user_id = tf_current_user()) WITH CHECK (user_id = tf_current_user());

ALTER TABLE messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE messages FORCE ROW LEVEL SECURITY;
CREATE POLICY messages_read ON messages FOR SELECT
  USING (tf_tenant_visible(tenant_id) AND tf_thread_member(thread_id));
CREATE POLICY messages_write ON messages FOR INSERT
  WITH CHECK (author_id = tf_current_user() AND tf_tenant_visible(tenant_id) AND tf_thread_member(thread_id));

GRANT SELECT, INSERT, UPDATE ON message_threads, thread_participants TO taskforce_app;
GRANT SELECT, INSERT ON messages TO taskforce_app;
GRANT EXECUTE ON FUNCTION tf_thread_member(uuid), tf_thread_creator(uuid) TO taskforce_app;
