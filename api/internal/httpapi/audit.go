package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
)

type auditEntry struct {
	Tenant     *uuid.UUID
	Workspace  *uuid.UUID
	Actor      uuid.UUID
	Action     string
	TargetType string
	TargetID   *uuid.UUID
	Summary    string
	Metadata   map[string]any
}

// audit appends an immutable event in the same transaction as the change it records.
func audit(ctx context.Context, tx pgx.Tx, e auditEntry) error {
	meta := e.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (tenant_id, workspace_id, actor_id, action, target_type, target_id, summary, metadata, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.Tenant, e.Workspace, e.Actor, e.Action, e.TargetType, e.TargetID, e.Summary, raw, requestID(ctx))
	if err != nil {
		return err
	}
	return enqueueWebhooks(ctx, tx, e, time.Now())
}

func (s *Server) auditEvents(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	org, err := pathUUID(r, "orgId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	scope, err := orgScope(actor, org)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !actor.Can(org, authz.AuditRead) {
		s.fail(w, r, errForbidden("Your role cannot review the audit trail."))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	type eventJSON struct {
		ID         uuid.UUID       `json:"id"`
		Actor      *userJSON       `json:"actor"`
		Action     string          `json:"action"`
		TargetType string          `json:"target_type"`
		TargetID   *uuid.UUID      `json:"target_id"`
		Summary    string          `json:"summary"`
		Metadata   json.RawMessage `json:"metadata"`
		RequestID  string          `json:"request_id"`
		OccurredAt time.Time       `json:"occurred_at"`
	}
	out := []eventJSON{}
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
			SELECT id, actor_id, action, target_type, target_id, summary, metadata, request_id, occurred_at
			FROM audit_events WHERE tenant_id = $1 ORDER BY occurred_at DESC LIMIT $2`, org, limit)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		var actors []uuid.UUID
		for rows.Next() {
			var e eventJSON
			var a uuid.UUID
			if err := rows.Scan(&e.ID, &a, &e.Action, &e.TargetType, &e.TargetID, &e.Summary, &e.Metadata, &e.RequestID, &e.OccurredAt); err != nil {
				rows.Close()
				return err
			}
			out = append(out, e)
			actors = append(actors, a)
			ids = append(ids, a)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		users, err := loadUsers(r.Context(), tx, ids)
		if err != nil {
			return err
		}
		for i := range out {
			if u, ok := users[actors[i]]; ok {
				out[i].Actor = &u
			}
		}
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out})
}
