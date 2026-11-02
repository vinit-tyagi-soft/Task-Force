-- Queue depth for /metrics. Returns counts only, never payloads or tenant ids.
-- The worker has no user context, so this is a definer function like tf_webhook_claim.

CREATE FUNCTION tf_webhook_stats() RETURNS TABLE (pending integer, failed integer)
  LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
  SELECT
    count(*) FILTER (WHERE status = 'PENDING')::integer,
    count(*) FILTER (WHERE status = 'FAILED')::integer
  FROM webhook_deliveries
$$;

REVOKE ALL ON FUNCTION tf_webhook_stats() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION tf_webhook_stats() TO taskforce_app;
