package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/authz"
	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

const (
	maxAttachmentBytes = 5 << 20
	maxChecklistItems  = 50
	maxDependencies    = 20
)

type checklistJSON struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	Done  bool      `json:"done"`
}

type linkedTaskJSON struct {
	ID     uuid.UUID `json:"id"`
	Key    string    `json:"key"`
	Title  string    `json:"title"`
	Status string    `json:"status"`
}

type attachmentJSON struct {
	ID          uuid.UUID `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int       `json:"size_bytes"`
	UploadedBy  *userJSON `json:"uploaded_by"`
	CreatedAt   time.Time `json:"created_at"`
	CanDelete   bool      `json:"can_delete"`
}

// readableLinks returns the linked tasks the caller may read; links to anything else are omitted.
func (s *Server) readableLinks(ctx context.Context, tx pgx.Tx, actor *authz.Actor, ids []uuid.UUID) ([]linkedTaskJSON, error) {
	out := []linkedTaskJSON{}
	for _, id := range ids {
		t, _, _, err := s.loadTask(ctx, tx, actor, id)
		var he *httpError
		if errors.As(err, &he) && he.status == http.StatusNotFound {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, linkedTaskJSON{ID: t.ID, Key: fmt.Sprintf("TF-%d", t.Number), Title: t.Title, Status: t.Status})
	}
	return out, nil
}

func collectIDs(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Server) taskDetails(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "taskId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	resp := map[string]any{}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		_, p, canEdit, err := s.loadTask(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		checklist := []checklistJSON{}
		rows, err := tx.Query(ctx, `SELECT id, title, done FROM task_checklist_items WHERE task_id = $1 ORDER BY created_at`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var c checklistJSON
			if err := rows.Scan(&c.ID, &c.Title, &c.Done); err != nil {
				rows.Close()
				return err
			}
			checklist = append(checklist, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		dependsOn, err := collectIDs(ctx, tx, `SELECT depends_on_id FROM task_dependencies WHERE task_id = $1 ORDER BY created_at`, id)
		if err != nil {
			return err
		}
		blocking, err := collectIDs(ctx, tx, `SELECT task_id FROM task_dependencies WHERE depends_on_id = $1 ORDER BY created_at`, id)
		if err != nil {
			return err
		}
		if resp["depends_on"], err = s.readableLinks(ctx, tx, actor, dependsOn); err != nil {
			return err
		}
		if resp["blocking"], err = s.readableLinks(ctx, tx, actor, blocking); err != nil {
			return err
		}

		attachments := []attachmentJSON{}
		var uploaders []uuid.UUID
		rows, err = tx.Query(ctx, `SELECT id, filename, content_type, size_bytes, uploaded_by, created_at FROM task_attachments
			WHERE task_id = $1 ORDER BY created_at`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var a attachmentJSON
			var by uuid.UUID
			if err := rows.Scan(&a.ID, &a.Filename, &a.ContentType, &a.Size, &by, &a.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			a.CanDelete = by == actor.UserID || p.Access == authz.AccessManage
			a.UploadedBy = &userJSON{ID: by}
			uploaders = append(uploaders, by)
			attachments = append(attachments, a)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		users, err := loadUsers(ctx, tx, uploaders)
		if err != nil {
			return err
		}
		for i := range attachments {
			if u, ok := users[attachments[i].UploadedBy.ID]; ok {
				attachments[i].UploadedBy = &u
			}
		}
		resp["checklist"] = checklist
		resp["attachments"] = attachments
		resp["can_edit"] = canEdit
		resp["can_attach"] = canEdit || p.Access >= authz.AccessContribute
		return nil
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, resp)
}

// editableTask loads a task and requires edit rights on it.
func (s *Server) editableTask(ctx context.Context, tx pgx.Tx, actor *authz.Actor, id uuid.UUID) (*taskRow, error) {
	t, _, canEdit, err := s.loadTask(ctx, tx, actor, id)
	if err != nil {
		return nil, err
	}
	if !canEdit {
		return nil, errForbidden("You can view this task but not change it.")
	}
	return t, nil
}

func (s *Server) addChecklistItem(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "taskId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Title string `json:"title"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" || len(in.Title) > 200 {
		s.fail(w, r, errValidation(map[string]string{"title": "Describe the step (up to 200 characters)."}))
		return
	}
	var out checklistJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		t, err := s.editableTask(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM task_checklist_items WHERE task_id = $1`, id).Scan(&n); err != nil {
			return err
		}
		if n >= maxChecklistItems {
			return errValidation(map[string]string{"title": fmt.Sprintf("A task can have up to %d checklist items.", maxChecklistItems)})
		}
		return tx.QueryRow(ctx, `INSERT INTO task_checklist_items (task_id, workspace_id, title, created_by) VALUES ($1, $2, $3, $4)
			RETURNING id, title, done`, id, t.WorkspaceID, in.Title, actor.UserID).Scan(&out.ID, &out.Title, &out.Done)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 201, out)
}

// checklistTask resolves the task behind a checklist item with edit rights.
func (s *Server) checklistTask(ctx context.Context, tx pgx.Tx, actor *authz.Actor, itemID uuid.UUID) error {
	var taskID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT task_id FROM task_checklist_items WHERE id = $1`, itemID).Scan(&taskID)
	if store.IsNoRows(err) {
		return errNotFound()
	}
	if err != nil {
		return err
	}
	_, err = s.editableTask(ctx, tx, actor, taskID)
	return err
}

func (s *Server) updateChecklistItem(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "itemId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Title *string `json:"title"`
		Done  *bool   `json:"done"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Title != nil {
		*in.Title = strings.TrimSpace(*in.Title)
		if *in.Title == "" || len(*in.Title) > 200 {
			s.fail(w, r, errValidation(map[string]string{"title": "Describe the step (up to 200 characters)."}))
			return
		}
	}
	var out checklistJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := s.checklistTask(ctx, tx, actor, id); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `UPDATE task_checklist_items SET title = coalesce($1, title), done = coalesce($2, done) WHERE id = $3
			RETURNING id, title, done`, in.Title, in.Done, id).Scan(&out.ID, &out.Title, &out.Done)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 200, out)
}

func (s *Server) deleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "itemId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := s.checklistTask(ctx, tx, actor, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM task_checklist_items WHERE id = $1`, id)
		return err
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addDependency(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "taskId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		DependsOn uuid.UUID `json:"depends_on_task_id"`
	}
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.DependsOn == id || in.DependsOn == uuid.Nil {
		s.fail(w, r, errValidation(map[string]string{"depends_on_task_id": "Choose a different task."}))
		return
	}
	var out linkedTaskJSON
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		t, err := s.editableTask(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		other, _, _, err := s.loadTask(ctx, tx, actor, in.DependsOn)
		if err != nil {
			return err
		}
		if other.WorkspaceID != t.WorkspaceID {
			return errValidation(map[string]string{"depends_on_task_id": "Dependencies must stay within the same workspace."})
		}
		var count int
		var cycle bool
		if err := tx.QueryRow(ctx, `
			WITH RECURSIVE chain(id) AS (
				SELECT depends_on_id FROM task_dependencies WHERE task_id = $2
				UNION SELECT d.depends_on_id FROM task_dependencies d JOIN chain c ON d.task_id = c.id)
			SELECT (SELECT count(*) FROM task_dependencies WHERE task_id = $1), EXISTS (SELECT 1 FROM chain WHERE id = $1)`,
			id, in.DependsOn).Scan(&count, &cycle); err != nil {
			return err
		}
		if cycle {
			return errValidation(map[string]string{"depends_on_task_id": "That would create a circular dependency."})
		}
		if count >= maxDependencies {
			return errValidation(map[string]string{"depends_on_task_id": fmt.Sprintf("A task can depend on up to %d tasks.", maxDependencies)})
		}
		tag, err := tx.Exec(ctx, `INSERT INTO task_dependencies (task_id, depends_on_id, workspace_id, created_by) VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING`, id, in.DependsOn, t.WorkspaceID, actor.UserID)
		if err != nil {
			return err
		}
		out = linkedTaskJSON{ID: other.ID, Key: fmt.Sprintf("TF-%d", other.Number), Title: other.Title, Status: other.Status}
		if tag.RowsAffected() == 0 {
			return nil
		}
		return audit(ctx, tx, auditEntry{Tenant: t.TenantID, Workspace: &t.WorkspaceID, Actor: actor.UserID, Action: "task.dependency_added",
			TargetType: "task", TargetID: &id, Summary: t.Title + " now depends on " + other.Title})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 201, out)
}

func (s *Server) removeDependency(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "taskId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	other, err := pathUUID(r, "dependsOnId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		t, err := s.editableTask(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM task_dependencies WHERE task_id = $1 AND depends_on_id = $2`, id, other)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errNotFound()
		}
		return audit(ctx, tx, auditEntry{Tenant: t.TenantID, Workspace: &t.WorkspaceID, Actor: actor.UserID, Action: "task.dependency_removed",
			TargetType: "task", TargetID: &id, Summary: "removed a dependency from " + t.Title})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func cleanFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`"<>:|?*/`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	if len([]rune(name)) > 200 {
		name = string([]rune(name)[:200])
	}
	return name
}

func (s *Server) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "taskId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			s.fail(w, r, errValidation(map[string]string{"file": "Files can be up to 5 MB."}))
			return
		}
		s.fail(w, r, errValidation(map[string]string{"file": "Attach a file in the \"file\" form field."}))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAttachmentBytes+1))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(data) == 0 || len(data) > maxAttachmentBytes {
		s.fail(w, r, errValidation(map[string]string{"file": "Files must be between 1 byte and 5 MB."}))
		return
	}
	sum := sha256.Sum256(data)
	out := attachmentJSON{Filename: cleanFilename(header.Filename), ContentType: http.DetectContentType(data), Size: len(data), CanDelete: true}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		t, p, canEdit, err := s.loadTask(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if !canEdit && p.Access < authz.AccessContribute {
			return errForbidden("You can view this task but not attach files to it.")
		}
		if err := tx.QueryRow(ctx, `INSERT INTO task_attachments (task_id, workspace_id, filename, content_type, size_bytes, sha256, data, uploaded_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`, id, t.WorkspaceID, out.Filename, out.ContentType, out.Size,
			hex.EncodeToString(sum[:]), data, actor.UserID).Scan(&out.ID, &out.CreatedAt); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: t.TenantID, Workspace: &t.WorkspaceID, Actor: actor.UserID, Action: "attachment.added",
			TargetType: "task", TargetID: &id, Summary: "attached " + out.Filename + " to " + t.Title})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, r, 201, out)
}

// attachmentTask resolves an attachment's task with read rights and reports whether the caller may delete it.
func (s *Server) attachmentTask(ctx context.Context, tx pgx.Tx, actor *authz.Actor, id uuid.UUID) (*taskRow, string, bool, error) {
	var taskID, by uuid.UUID
	var name string
	err := tx.QueryRow(ctx, `SELECT task_id, uploaded_by, filename FROM task_attachments WHERE id = $1`, id).Scan(&taskID, &by, &name)
	if store.IsNoRows(err) {
		return nil, "", false, errNotFound()
	}
	if err != nil {
		return nil, "", false, err
	}
	t, p, _, err := s.loadTask(ctx, tx, actor, taskID)
	if err != nil {
		return nil, "", false, err
	}
	return t, name, by == actor.UserID || p.Access == authz.AccessManage, nil
}

func (s *Server) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "attachmentId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var name, ctype string
	var data []byte
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		if _, _, _, err := s.attachmentTask(ctx, tx, actor, id); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT filename, content_type, data FROM task_attachments WHERE id = $1`, id).Scan(&name, &ctype, &data)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Content-Length", fmt.Sprint(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r.Context())
	id, err := pathUUID(r, "attachmentId")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	err = s.store.InTx(r.Context(), store.Scope{UserID: actor.UserID}, func(tx pgx.Tx) error {
		ctx := r.Context()
		t, name, canDelete, err := s.attachmentTask(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if !canDelete {
			return errForbidden("Only the uploader or a project manager can remove this file.")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM task_attachments WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(ctx, tx, auditEntry{Tenant: t.TenantID, Workspace: &t.WorkspaceID, Actor: actor.UserID, Action: "attachment.removed",
			TargetType: "task", TargetID: &t.ID, Summary: "removed " + name + " from " + t.Title})
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
