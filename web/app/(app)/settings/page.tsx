"use client";

import { useApi } from "@/lib/api";
import { roleLabel } from "@/lib/format";
import type { AuditEvent, List } from "@/lib/types";
import { Avatar, Card, Empty, Heading, LoadError, Loading, Notice } from "@/components/ui";
import { useSession } from "@/components/session";

export default function Settings() {
  const { me, activeOrg, refreshKey, can } = useSession();
  const canAudit = can("audit.read");
  const audit = useApi<List<AuditEvent>>(activeOrg && canAudit ? `/organizations/${activeOrg.id}/audit-events?limit=100` : null, refreshKey);

  return (
    <>
      <Heading title="Settings & administration" sub="Your profile, organization memberships and the audit trail." />
      <div className="dashboard-grid">
        <div>
          <Card title="Audit trail" sub={activeOrg ? `${activeOrg.name} · append-only, newest first` : undefined}>
            {!activeOrg ? (
              <Empty title="No organization selected" desc="Audit events are scoped to an organization." />
            ) : !canAudit ? (
              <Notice>Your role in {activeOrg.name} does not include audit review. Tenant owners, org admins and auditors can see this trail.</Notice>
            ) : audit.error ? (
              <LoadError message={audit.error.message} onRetry={audit.reload} />
            ) : !audit.data ? (
              <Loading />
            ) : audit.data.items.length === 0 ? (
              <Empty title="No events yet" desc="Changes to projects and tasks will appear here." />
            ) : (
              <div className="table-wrap">
                <table>
                  <thead><tr><th>When</th><th>Actor</th><th>Action</th><th>Summary</th><th>Request</th></tr></thead>
                  <tbody>
                    {audit.data.items.map((e) => (
                      <tr key={e.id}>
                        <td className="tiny">{new Date(e.occurred_at).toLocaleString("en-GB", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}</td>
                        <td><div className="row"><Avatar user={e.actor} size="sm" /><span className="tiny">{e.actor?.display_name}</span></div></td>
                        <td><code className="tiny">{e.action}</code></td>
                        <td className="tiny">{e.summary}</td>
                        <td className="tiny muted" title={e.request_id}>{e.request_id.slice(0, 8)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </div>
        <div>
          <Card title="Your profile">
            <div className="row" style={{ marginBottom: 14 }}>
              <Avatar user={me.user} size="lg" />
              <div>
                <h3>{me.user.display_name}</h3>
                <p>{me.user.title}</p>
                <p className="tiny muted">{me.user.email} · {me.user.timezone}</p>
              </div>
            </div>
          </Card>
          <Card title="Memberships">
            {me.organizations.map((o) => (
              <div className="settings-row" key={o.id}>
                <h3>{o.name}</h3>
                <span className="tiny muted">{o.roles.map((r) => roleLabel[r] ?? r).join(", ")}</span>
              </div>
            ))}
            <p className="tiny muted" style={{ marginTop: 12 }}>Roles are granted by organization administrators and enforced by the server on every request.</p>
          </Card>
        </div>
      </div>
    </>
  );
}
