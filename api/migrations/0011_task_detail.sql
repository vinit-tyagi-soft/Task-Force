-- Task detail: checklist items, dependency links and file attachments. Composite keys on
-- (task, workspace) keep every link and file inside the task's own workspace.
ALTER TABLE tasks ADD CONSTRAINT tasks_id_workspace_key UNIQUE (id, workspace_id);

CREATE TABLE task_checklist_items (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id      uuid NOT NULL,
  workspace_id uuid NOT NULL,
  title        text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
  done         boolean NOT NULL DEFAULT false,
  created_by   uuid NOT NULL REFERENCES users (id),
  created_at   timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (task_id, workspace_id) REFERENCES tasks (id, workspace_id) ON DELETE CASCADE
);
CREATE INDEX task_checklist_items_task_idx ON task_checklist_items (task_id, created_at);

CREATE TABLE task_dependencies (
  task_id       uuid NOT NULL,
  depends_on_id uuid NOT NULL,
  workspace_id  uuid NOT NULL,
  created_by    uuid NOT NULL REFERENCES users (id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (task_id, depends_on_id),
  CHECK (task_id <> depends_on_id),
  FOREIGN KEY (task_id, workspace_id) REFERENCES tasks (id, workspace_id) ON DELETE CASCADE,
  FOREIGN KEY (depends_on_id, workspace_id) REFERENCES tasks (id, workspace_id) ON DELETE CASCADE
);
CREATE INDEX task_dependencies_depends_on_idx ON task_dependencies (depends_on_id);

CREATE TABLE task_attachments (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id      uuid NOT NULL,
  workspace_id uuid NOT NULL,
  filename     text NOT NULL CHECK (length(filename) BETWEEN 1 AND 200),
  content_type text NOT NULL,
  size_bytes   integer NOT NULL CHECK (size_bytes BETWEEN 1 AND 5242880),
  sha256       text NOT NULL,
  data         bytea NOT NULL,
  uploaded_by  uuid NOT NULL REFERENCES users (id),
  created_at   timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (task_id, workspace_id) REFERENCES tasks (id, workspace_id) ON DELETE CASCADE
);
CREATE INDEX task_attachments_task_idx ON task_attachments (task_id, created_at);

ALTER TABLE task_checklist_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_checklist_items FORCE ROW LEVEL SECURITY;
CREATE POLICY task_checklist_items_scope ON task_checklist_items FOR ALL
  USING (tf_workspace_visible(workspace_id)) WITH CHECK (tf_workspace_visible(workspace_id));

ALTER TABLE task_dependencies ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_dependencies FORCE ROW LEVEL SECURITY;
CREATE POLICY task_dependencies_scope ON task_dependencies FOR ALL
  USING (tf_workspace_visible(workspace_id)) WITH CHECK (tf_workspace_visible(workspace_id));

ALTER TABLE task_attachments ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_attachments FORCE ROW LEVEL SECURITY;
CREATE POLICY task_attachments_scope ON task_attachments FOR ALL
  USING (tf_workspace_visible(workspace_id))
  WITH CHECK (tf_workspace_visible(workspace_id) AND uploaded_by = tf_current_user());

GRANT SELECT, INSERT, UPDATE, DELETE ON task_checklist_items TO taskforce_app;
GRANT SELECT, INSERT, DELETE ON task_dependencies, task_attachments TO taskforce_app;
