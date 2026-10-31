-- Administration: tenant creation, invitations, role and membership changes, org settings.
-- Privileged inserts go through definer functions that re-check the caller's role themselves.

CREATE FUNCTION tf_org_admin(p_org uuid) RETURNS boolean
  LANGUAGE sql STABLE AS $$ SELECT tf_has_role(p_org, ARRAY['TENANT_OWNER', 'ORG_ADMIN']) $$;

CREATE FUNCTION tf_create_org(p_slug text, p_name text, p_short text, p_tz text) RETURNS uuid
  LANGUAGE plpgsql SECURITY DEFINER SET search_path = public AS $$
DECLARE
  v_user uuid := tf_current_user();
  v_org  uuid;
BEGIN
  IF v_user IS NULL OR NOT EXISTS (SELECT 1 FROM users WHERE id = v_user AND state = 'ACTIVE') THEN
    RAISE EXCEPTION 'not authenticated' USING ERRCODE = '42501';
  END IF;
  IF EXISTS (SELECT 1 FROM organizations WHERE slug = p_slug) THEN
    RAISE EXCEPTION 'that URL name is already taken' USING ERRCODE = '23514';
  END IF;
  INSERT INTO organizations (slug, name, short_name, region, reporting_timezone)
    VALUES (p_slug, p_name, p_short, 'Default', p_tz) RETURNING id INTO v_org;
  INSERT INTO org_memberships (org_id, user_id, state, joined_at) VALUES (v_org, v_user, 'ACTIVE', now());
  INSERT INTO role_bindings (tenant_id, user_id, role_key, created_by) VALUES (v_org, v_user, 'TENANT_OWNER', v_user);
  INSERT INTO workspaces (kind, tenant_id, name) VALUES ('ORG', v_org, p_name);
  RETURN v_org;
END $$;

CREATE FUNCTION tf_invite_member(p_org uuid, p_email text, p_name text, p_initials text, p_role text) RETURNS uuid
  LANGUAGE plpgsql SECURITY DEFINER SET search_path = public AS $$
DECLARE
  v_user  uuid;
  v_state text;
BEGIN
  IF NOT tf_org_admin(p_org) OR (p_role = 'TENANT_OWNER' AND NOT tf_has_role(p_org, ARRAY['TENANT_OWNER'])) THEN
    RAISE EXCEPTION 'not allowed to invite' USING ERRCODE = '42501';
  END IF;
  SELECT id INTO v_user FROM users WHERE lower(email) = lower(p_email);
  IF v_user IS NULL THEN
    INSERT INTO users (email, display_name, initials) VALUES (p_email, p_name, p_initials) RETURNING id INTO v_user;
    INSERT INTO workspaces (kind, owner_user_id, name) VALUES ('PERSONAL', v_user, 'Personal workspace');
  END IF;
  SELECT state INTO v_state FROM org_memberships WHERE org_id = p_org AND user_id = v_user;
  IF v_state IS NULL THEN
    INSERT INTO org_memberships (org_id, user_id, state) VALUES (p_org, v_user, 'INVITED');
  ELSIF v_state = 'REMOVED' THEN
    DELETE FROM role_bindings WHERE tenant_id = p_org AND user_id = v_user;
    UPDATE org_memberships SET state = 'INVITED', joined_at = NULL WHERE org_id = p_org AND user_id = v_user;
  ELSE
    RAISE EXCEPTION 'that person is already a member or has a pending invitation' USING ERRCODE = '23514';
  END IF;
  INSERT INTO role_bindings (tenant_id, user_id, role_key, created_by) VALUES (p_org, v_user, p_role, tf_current_user());
  RETURN v_user;
END $$;

CREATE FUNCTION tf_my_invitations() RETURNS TABLE (org_id uuid, name text, invited_at timestamptz)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT o.id, o.name, m.created_at FROM org_memberships m JOIN organizations o ON o.id = m.org_id
  WHERE m.user_id = tf_current_user() AND m.state = 'INVITED' AND o.status = 'ACTIVE' ORDER BY m.created_at
$$;

CREATE FUNCTION tf_accept_invitation(p_org uuid) RETURNS boolean
  LANGUAGE plpgsql SECURITY DEFINER SET search_path = public AS $$
BEGIN
  UPDATE org_memberships SET state = 'ACTIVE', joined_at = now()
    WHERE org_id = p_org AND user_id = tf_current_user() AND state = 'INVITED';
  RETURN FOUND;
END $$;

REVOKE EXECUTE ON FUNCTION tf_create_org(text, text, text, text), tf_invite_member(uuid, text, text, text, text),
  tf_my_invitations(), tf_accept_invitation(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION tf_org_admin(uuid), tf_create_org(text, text, text, text), tf_invite_member(uuid, text, text, text, text),
  tf_my_invitations(), tf_accept_invitation(uuid) TO taskforce_app;

CREATE POLICY organizations_admin_update ON organizations FOR UPDATE
  USING (tf_tenant_visible(id) AND tf_org_admin(id)) WITH CHECK (tf_tenant_visible(id) AND tf_org_admin(id));
CREATE POLICY org_memberships_admin_update ON org_memberships FOR UPDATE
  USING (tf_tenant_visible(org_id) AND tf_org_admin(org_id)) WITH CHECK (tf_tenant_visible(org_id) AND tf_org_admin(org_id));
CREATE POLICY role_bindings_admin_insert ON role_bindings FOR INSERT
  WITH CHECK (tf_tenant_visible(tenant_id) AND tf_org_admin(tenant_id));
CREATE POLICY role_bindings_admin_delete ON role_bindings FOR DELETE
  USING (tf_tenant_visible(tenant_id) AND tf_org_admin(tenant_id));

GRANT UPDATE (name, reporting_timezone) ON organizations TO taskforce_app;
GRANT UPDATE (state, joined_at) ON org_memberships TO taskforce_app;
GRANT INSERT, DELETE ON role_bindings TO taskforce_app;
