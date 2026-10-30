package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

const maxThreadParticipants = 50

type threadJSON struct {
	ID            uuid.UUID     `json:"id"`
	Subject       string        `json:"subject"`
	Project       *taskProj     `json:"project"`
	Participants  []userJSON    `json:"participants"`
	LastAuthor    *userJSON     `json:"last_author"`
	Excerpt       string        `json:"excerpt"`
	LastMessageAt time.Time     `json:"last_message_at"`
	MessageCount  int           `json:"message_count"`
	Unread        bool          `json:"unread"`
	Starred       bool          `json:"starred"`
	Messages      []messageJSON `json:"messages,omitempty"`
}

type messageJSON struct {
	ID        uuid.UUID `json:"id"`
	Author    *userJSON `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// loadThreads returns the caller's threads in org. RLS limits rows to threads they participate in;
// a linked project is only named when the caller can read it.
func loadThreads(ctx context.Context, tx pgx.Tx, actor *authz.Actor, org uuid.UUID, filter string, only *uuid.UUID) ([]threadJSON, error) {
	rows, err := tx.Query(ctx, `
		SELECT t.id, t.subject, t.project_id, t.last_message_at, me.starred,
		       (me.last_read_at IS NULL OR me.last_read_at < t.last_message_at),
		       (SELECT count(*) FROM messages m WHERE m.thread_id = t.id),
		       lm.author_id, coalesce(lm.body, ''),
		       (SELECT array_agg(tp.user_id) FROM thread_participants tp WHERE tp.thread_id = t.id)
		FROM message_threads t
		JOIN thread_participants me ON me.thread_id = t.id AND me.user_id = $2
		LEFT JOIN LATERAL (SELECT author_id, body FROM messages m WHERE m.thread_id = t.id ORDER BY created_at DESC LIMIT 1) lm ON true
		WHERE t.tenant_id = $1 AND ($3::uuid IS NULL OR t.id = $3)
		  AND ($4 <> 'starred' OR me.starred)
		  AND ($4 <> 'unread' OR me.last_read_at IS NULL OR me.last_read_at < t.last_message_at)
		ORDER BY t.last_message_at DESC LIMIT 100`, org, actor.UserID, only, filter)
	if err != nil {
		return nil, err
	}
	type raw struct {
		threadJSON
		projectID    *uuid.UUID
		lastAuthor   *uuid.UUID
		participants []uuid.UUID
	}
	var list []raw
	var userIDs, projectIDs []uuid.UUID
	for rows.Next() {
		var t raw
		if err := rows.Scan(&t.ID, &t.Subject, &t.projectID, &t.LastMessageAt, &t.Starred, &t.Unread, &t.MessageCount,
			&t.lastAuthor, &t.Excerpt, &t.participants); err != nil {
			rows.Close()
			return nil, err
		}
		t.Excerpt = excerpt(t.Excerpt, 120)
		userIDs = append(userIDs, t.participants...)
		if t.lastAuthor != nil {
			userIDs = append(userIDs, *t.lastAuthor)
		}
		if t.projectID != nil {
			projectIDs = append(projectIDs, *t.projectID)
		}
		list = append(list, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	users, err := loadUsers(ctx, tx, userIDs)
	if err != nil {
		return nil, err
	}
	projects := map[uuid.UUID]taskProj{}
	if len(projectIDs) > 0 {
		prows, err := tx.Query(ctx, projectSelect+` AND p.id = ANY($2)`, actor.UserID, projectIDs)
		if err != nil {
			return nil, err
		}
		for prows.Next() {
			p, err := scanProject(prows, actor)
			if err != nil {
				prows.Close()
				return nil, err
			}
			if p.Access >= authz.AccessRead {
				projects[p.ID] = taskProj{ID: p.ID, Name: p.Name, Color: p.Color}
			}
		}
		prows.Close()
		if err := prows.Err(); err != nil {
			return nil, err
		}
	}
	out := make([]threadJSON, 0, len(list))
	for _, t := range list {
		t.Participants = []userJSON{}
		for _, id := range t.participants {
			if u, ok := users[id]; ok {
				t.Participants = append(t.Participants, u)
			}
		}
		sortMembers(t.Participants, func(u userJSON) string { return u.DisplayName })
		if t.lastAuthor != nil {
			if u, ok := users[*t.lastAuthor]; ok {
				t.LastAuthor = &u
			}
		}
		if t.projectID != nil {
			if p, ok := projects[*t.projectID]; ok {
				t.Project = &p
			}
		}
		out = append(out, t.threadJSON)
	}
	return out, nil
}

func (s *Server) listThreads(w http.ResponseWriter, r *http.Request) {
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
	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "inbox"
	}
	if filter != "inbox" && filter != "starred" && filter != "unread" {
		s.fail(w, r, errValidation(map[string]string{"filter": "Use inbox, starred or unread."}))
		return
	}
	var out []threadJSON
	var unread int
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		if out, err = loadThreads(ctx, tx, actor, org, filter, nil); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM message_threads t JOIN thread_participants me ON me.thread_id = t.id AND me.user_id = $2
			WHERE t.tenant_id = $1 AND (me.last_read_at IS NULL OR me.last_read_at < t.last_message_at)`, org, actor.UserID).Scan(&unread)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"items": out, "unread": unread})
}

// threadTenant resolves a visible thread's org; RLS hides threads the caller is not part of.
func threadTenant(ctx context.Context, tx pgx.Tx, actor *authz.Actor, id uuid.UUID) (uuid.UUID, error) {
	var org uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT tenant_id FROM message_threads WHERE id = $1`, id).Scan(&org); err != nil {
		if store.IsNoRows(err) {
			return org, errNotFound()
		}
		return org, err
	}
	if !actor.IsMember(org) {
		return org, errNotFound()
	}
	return org, nil
}

func (s *Server) renderThread(ctx context.Context, tx pgx.Tx, actor *authz.Actor, org, id uuid.UUID) (threadJSON, error) {
	list, err := loadThreads(ctx, tx, actor, org, "inbox", &id)
	if err != nil {
		return threadJSON{}, err
	}
	if len(list) == 0 {
		return threadJSON{}, errNotFound()
	}
	t := list[0]
	rows, err := tx.Query(ctx, `SELECT id, author_id, body, created_at FROM messages WHERE thread_id = $1 ORDER BY created_at, id`, id)
	if err != nil {
		return t, err
	}
	var authors []uuid.UUID
	type raw struct {
		messageJSON
		author uuid.UUID
	}
	var msgs []raw
	for rows.Next() {
		var m raw
		if err := rows.Scan(&m.ID, &m.author, &m.Body, &m.CreatedAt); err != nil {
			rows.Close()
			return t, err
		}
		authors = append(authors, m.author)
		msgs = append(msgs, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return t, err
	}
	users, err := loadUsers(ctx, tx, authors)
	if err != nil {
		return t, err
	}
	t.Messages = make([]messageJSON, 0, len(msgs))
	for _, m := range msgs {
		if u, ok := users[m.author]; ok {
			m.Author = &u
		}
		t.Messages = append(t.Messages, m.messageJSON)
	}
	return t, nil
}

func (s *Server) getThread(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "threadId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var out threadJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		org, err := threadTenant(r.Context(), tx, actor, id)
		if err != nil {
			return err
		}
		out, err = s.renderThread(r.Context(), tx, actor, org, id)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, out)
}

func validBody(body string, errs map[string]string) string {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > 10000 {
		errs["body"] = "Write a message (up to 10,000 characters)."
	}
	return body
}

func (s *Server) createThread(w http.ResponseWriter, r *http.Request) {
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
	var in struct {
		Subject        string      `json:"subject"`
		Body           string      `json:"body"`
		ParticipantIDs []uuid.UUID `json:"participant_ids"`
		ProjectID      *uuid.UUID  `json:"project_id"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	errs := map[string]string{}
	in.Subject = strings.TrimSpace(in.Subject)
	if in.Subject == "" || utf8.RuneCountInString(in.Subject) > 160 {
		errs["subject"] = "Add a subject (up to 160 characters)."
	}
	in.Body = validBody(in.Body, errs)
	var others []uuid.UUID
	for _, id := range uniqueIDs(in.ParticipantIDs...) {
		if id != actor.UserID {
			others = append(others, id)
		}
	}
	if len(others) == 0 || len(others) > maxThreadParticipants {
		errs["participant_ids"] = "Choose between 1 and " + strconv.Itoa(maxThreadParticipants) + " people."
	}
	if len(errs) > 0 {
		s.fail(w, r, errValidation(errs))
		return
	}
	var out threadJSON
	err = s.store.InTx(r.Context(), scope, func(tx pgx.Tx) error {
		ctx := r.Context()
		var active int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM org_memberships WHERE org_id = $1 AND state = 'ACTIVE' AND user_id = ANY($2)`, org, others).Scan(&active); err != nil {
			return err
		}
		if active != len(others) {
			return errValidation(map[string]string{"participant_ids": "Everyone in the conversation must be an active member of this organization."})
		}
		if in.ProjectID != nil {
			p, err := loadProject(ctx, tx, actor, *in.ProjectID)
			if err != nil || p.TenantID == nil || *p.TenantID != org {
				return errValidation(map[string]string{"project_id": "Link a project you can see in this organization."})
			}
		}
		id := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO message_threads (id, tenant_id, subject, project_id, created_by) VALUES ($1, $2, $3, $4, $5)`,
			id, org, in.Subject, in.ProjectID, actor.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO thread_participants (thread_id, tenant_id, user_id, last_read_at)
			SELECT $1, $2, u, CASE WHEN u = $3 THEN now() END FROM unnest($4::uuid[]) u`, id, org, actor.UserID, append(others, actor.UserID)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO messages (thread_id, tenant_id, author_id, body) VALUES ($1, $2, $3, $4)`, id, org, actor.UserID, in.Body); err != nil {
			return err
		}
		if err := audit(ctx, tx, auditEntry{Tenant: &org, Actor: actor.UserID, Action: "thread.created", TargetType: "thread", TargetID: &id,
			Summary: "started a conversation with " + strconv.Itoa(len(others)) + " people"}); err != nil {
			return err
		}
		out, err = s.renderThread(ctx, tx, actor, org, id)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 201, out)
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "threadId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	errs := map[string]string{}
	in.Body = validBody(in.Body, errs)
	if len(errs) > 0 {
		s.fail(w, r, errValidation(errs))
		return
	}
	var out threadJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		org, err := threadTenant(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO messages (thread_id, tenant_id, author_id, body) VALUES ($1, $2, $3, $4)`, id, org, actor.UserID, in.Body); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE message_threads SET last_message_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE thread_participants SET last_read_at = now() WHERE thread_id = $1 AND user_id = $2`, id, actor.UserID); err != nil {
			return err
		}
		out, err = s.renderThread(ctx, tx, actor, org, id)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 201, out)
}

func (s *Server) setThreadState(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "threadId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Starred *bool `json:"starred"`
		Read    *bool `json:"read"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		if _, err := threadTenant(ctx, tx, actor, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE thread_participants SET starred = coalesce($3, starred),
			last_read_at = CASE WHEN $4::boolean IS NULL THEN last_read_at WHEN $4 THEN now() ELSE NULL END
			WHERE thread_id = $1 AND user_id = $2`, id, actor.UserID, in.Starred, in.Read)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
