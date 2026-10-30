-- Development & Hiring: requisitions and candidates for HR and hiring managers,
-- private development goals for everyone.

CREATE FUNCTION tf_has_role(p_org uuid, p_roles text[]) RETURNS boolean
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT tf_is_member(p_org) AND EXISTS (
    SELECT 1 FROM role_bindings WHERE tenant_id = p_org AND user_id = tf_current_user() AND role_key = ANY(p_roles))
$$;

CREATE TABLE job_requisitions (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id         uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
  title             text NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
  department_id     uuid,
  hiring_manager_id uuid NOT NULL,
  openings          integer NOT NULL DEFAULT 1 CHECK (openings BETWEEN 1 AND 50),
  stage             text NOT NULL DEFAULT 'PENDING_APPROVAL'
                    CHECK (stage IN ('PENDING_APPROVAL', 'APPROVED', 'SCREENING', 'INTERVIEWING', 'OFFER', 'CLOSED')),
  version           integer NOT NULL DEFAULT 1,
  created_by        uuid NOT NULL REFERENCES users (id),
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, tenant_id),
  FOREIGN KEY (department_id, tenant_id) REFERENCES departments (id, tenant_id),
  FOREIGN KEY (tenant_id, hiring_manager_id) REFERENCES org_memberships (org_id, user_id)
);

CREATE TABLE candidates (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id         uuid NOT NULL,
  requisition_id    uuid NOT NULL,
  full_name         text NOT NULL CHECK (length(full_name) BETWEEN 1 AND 120),
  email             text NOT NULL DEFAULT '' CHECK (length(email) <= 320),
  stage             text NOT NULL DEFAULT 'APPLIED'
                    CHECK (stage IN ('APPLIED', 'SCREEN', 'INTERVIEW', 'OFFER', 'HIRED', 'REJECTED')),
  next_interview_at timestamptz,
  notes             text NOT NULL DEFAULT '' CHECK (length(notes) <= 2000),
  version           integer NOT NULL DEFAULT 1,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (requisition_id, tenant_id) REFERENCES job_requisitions (id, tenant_id) ON DELETE CASCADE
);
CREATE INDEX candidates_requisition_idx ON candidates (requisition_id);

CREATE TABLE development_goals (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  uuid NOT NULL,
  user_id    uuid NOT NULL,
  title      text NOT NULL CHECK (length(title) BETWEEN 1 AND 160),
  target_on  date,
  progress   integer NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (tenant_id, user_id) REFERENCES org_memberships (org_id, user_id) ON DELETE CASCADE
);
CREATE INDEX development_goals_user_idx ON development_goals (user_id, tenant_id);

CREATE FUNCTION tf_hiring_admin(p_org uuid) RETURNS boolean
  LANGUAGE sql STABLE AS $$ SELECT tf_has_role(p_org, ARRAY['HR_SPECIALIST', 'ORG_ADMIN', 'TENANT_OWNER']) $$;

CREATE FUNCTION tf_requisition_visible(p_req uuid) RETURNS boolean
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT EXISTS (SELECT 1 FROM job_requisitions r WHERE r.id = p_req
    AND tf_tenant_visible(r.tenant_id) AND (tf_hiring_admin(r.tenant_id) OR r.hiring_manager_id = tf_current_user()))
$$;

ALTER TABLE job_requisitions ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_requisitions FORCE ROW LEVEL SECURITY;
CREATE POLICY job_requisitions_read ON job_requisitions FOR SELECT
  USING (tf_tenant_visible(tenant_id) AND (tf_hiring_admin(tenant_id) OR hiring_manager_id = tf_current_user()));
CREATE POLICY job_requisitions_write ON job_requisitions FOR INSERT
  WITH CHECK (tf_tenant_visible(tenant_id) AND tf_hiring_admin(tenant_id));
CREATE POLICY job_requisitions_update ON job_requisitions FOR UPDATE
  USING (tf_tenant_visible(tenant_id) AND tf_hiring_admin(tenant_id))
  WITH CHECK (tf_tenant_visible(tenant_id) AND tf_hiring_admin(tenant_id));

ALTER TABLE candidates ENABLE ROW LEVEL SECURITY;
ALTER TABLE candidates FORCE ROW LEVEL SECURITY;
CREATE POLICY candidates_read ON candidates FOR SELECT USING (tf_requisition_visible(requisition_id));
CREATE POLICY candidates_write ON candidates FOR INSERT
  WITH CHECK (tf_tenant_visible(tenant_id) AND tf_hiring_admin(tenant_id));
CREATE POLICY candidates_update ON candidates FOR UPDATE
  USING (tf_tenant_visible(tenant_id) AND tf_hiring_admin(tenant_id))
  WITH CHECK (tf_tenant_visible(tenant_id) AND tf_hiring_admin(tenant_id));

ALTER TABLE development_goals ENABLE ROW LEVEL SECURITY;
ALTER TABLE development_goals FORCE ROW LEVEL SECURITY;
CREATE POLICY development_goals_own ON development_goals FOR ALL
  USING (user_id = tf_current_user() AND tf_tenant_visible(tenant_id))
  WITH CHECK (user_id = tf_current_user() AND tf_tenant_visible(tenant_id));

GRANT SELECT, INSERT, UPDATE ON job_requisitions, candidates TO taskforce_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON development_goals TO taskforce_app;
GRANT EXECUTE ON FUNCTION tf_has_role(uuid, text[]), tf_hiring_admin(uuid), tf_requisition_visible(uuid) TO taskforce_app;
