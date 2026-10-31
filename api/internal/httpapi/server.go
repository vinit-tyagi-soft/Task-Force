// Package httpapi exposes the /api/v1 REST contract.
package httpapi

import (
	"context"
	"encoding/json"
	_ "embed"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/vinit-tyagi-soft/taskforce/api/internal/store"
)

//go:embed openapi.json
var openAPIDocument []byte

func (s *Server) openAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.oai.openapi+json;version=3.1")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPIDocument)
}

type Config struct {
	SessionSecret []byte
	DevAuth       bool
	SecureCookies bool
	SessionTTL    time.Duration
	// WebhookTransport overrides the SSRF-guarded transport (tests only).
	WebhookTransport http.RoundTripper
}

type Server struct {
	store         *store.Store
	cfg           Config
	log           *slog.Logger
	now           func() time.Time
	webhookClient *http.Client
}

func New(s *store.Store, cfg Config, log *slog.Logger) *Server {
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 8 * time.Hour
	}
	transport := cfg.WebhookTransport
	if transport == nil {
		transport = guardedTransport()
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Server{store: s, cfg: cfg, log: log, now: time.Now, webhookClient: client}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, r, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /openapi.json", s.openAPI)

	if s.cfg.DevAuth {
		mux.HandleFunc("POST /api/v1/dev/login", s.devLogin)
	}
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)

	a := s.authenticated
	mux.Handle("GET /api/v1/me", a(s.getMe))
	mux.Handle("GET /api/v1/me/work-items", a(s.myWorkItems))
	mux.Handle("GET /api/v1/me/calendar", a(s.calendar))
	mux.Handle("GET /api/v1/workspaces", a(s.listWorkspaces))
	mux.Handle("GET /api/v1/workspaces/{workspaceId}/projects", a(s.listProjects))
	mux.Handle("POST /api/v1/workspaces/{workspaceId}/projects", a(s.createProject))
	mux.Handle("GET /api/v1/projects/{projectId}", a(s.getProject))
	mux.Handle("PATCH /api/v1/projects/{projectId}", a(s.patchProject))
	mux.Handle("GET /api/v1/projects/{projectId}/members", a(s.projectMembers))
	mux.Handle("GET /api/v1/projects/{projectId}/tasks", a(s.listProjectTasks))
	mux.Handle("POST /api/v1/projects/{projectId}/tasks", a(s.createTask))
	mux.Handle("GET /api/v1/projects/{projectId}/board", a(s.projectBoard))
	mux.Handle("GET /api/v1/tasks/{taskId}", a(s.getTask))
	mux.Handle("PATCH /api/v1/tasks/{taskId}", a(s.patchTask))
	mux.Handle("GET /api/v1/tasks/{taskId}/comments", a(s.listComments))
	mux.Handle("POST /api/v1/tasks/{taskId}/comments", a(s.createComment))
	mux.Handle("GET /api/v1/organizations/{orgId}", a(s.getOrganization))
	mux.Handle("GET /api/v1/organizations/{orgId}/members", a(s.orgMembers))
	mux.Handle("GET /api/v1/organizations/{orgId}/departments", a(s.orgDepartments))
	mux.Handle("GET /api/v1/organizations/{orgId}/teams", a(s.listTeams))
	mux.Handle("POST /api/v1/organizations/{orgId}/teams", a(s.createTeam))
	mux.Handle("PUT /api/v1/teams/{teamId}", a(s.updateTeam))
	mux.Handle("DELETE /api/v1/teams/{teamId}", a(s.deleteTeam))
	mux.Handle("GET /api/v1/organizations/{orgId}/workload", a(s.workload))
	mux.Handle("GET /api/v1/organizations/{orgId}/automation-rules", a(s.listRules))
	mux.Handle("POST /api/v1/organizations/{orgId}/automation-rules", a(s.createRule))
	mux.Handle("PUT /api/v1/automation-rules/{ruleId}", a(s.updateRule))
	mux.Handle("DELETE /api/v1/automation-rules/{ruleId}", a(s.deleteRule))
	mux.Handle("POST /api/v1/organizations/{orgId}/automation-runs", a(s.runRules))
	mux.Handle("GET /api/v1/organizations/{orgId}/automation-runs", a(s.listRuns))
	mux.Handle("GET /api/v1/organizations/{orgId}/notifications", a(s.listNotifications))
	mux.Handle("POST /api/v1/organizations/{orgId}/notifications/read", a(s.readNotifications))
	mux.Handle("GET /api/v1/organizations/{orgId}/api-tokens", a(s.listTokens))
	mux.Handle("POST /api/v1/organizations/{orgId}/api-tokens", a(s.createToken))
	mux.Handle("POST /api/v1/api-tokens/{tokenId}/revoke", a(s.revokeToken))
	mux.Handle("GET /api/v1/organizations/{orgId}/webhooks", a(s.listWebhooks))
	mux.Handle("POST /api/v1/organizations/{orgId}/webhooks", a(s.createWebhook))
	mux.Handle("PUT /api/v1/webhooks/{webhookId}", a(s.updateWebhook))
	mux.Handle("DELETE /api/v1/webhooks/{webhookId}", a(s.deleteWebhook))
	mux.Handle("POST /api/v1/webhooks/{webhookId}/test", a(s.testWebhook))
	mux.Handle("GET /api/v1/organizations/{orgId}/webhook-deliveries", a(s.listDeliveries))
	mux.Handle("GET /api/v1/tasks/{taskId}/details", a(s.taskDetails))
	mux.Handle("POST /api/v1/tasks/{taskId}/checklist", a(s.addChecklistItem))
	mux.Handle("PATCH /api/v1/checklist-items/{itemId}", a(s.updateChecklistItem))
	mux.Handle("DELETE /api/v1/checklist-items/{itemId}", a(s.deleteChecklistItem))
	mux.Handle("POST /api/v1/tasks/{taskId}/dependencies", a(s.addDependency))
	mux.Handle("DELETE /api/v1/tasks/{taskId}/dependencies/{dependsOnId}", a(s.removeDependency))
	mux.Handle("POST /api/v1/tasks/{taskId}/attachments", a(s.uploadAttachment))
	mux.Handle("GET /api/v1/attachments/{attachmentId}", a(s.downloadAttachment))
	mux.Handle("DELETE /api/v1/attachments/{attachmentId}", a(s.deleteAttachment))
	mux.Handle("POST /api/v1/projects/{projectId}/milestones", a(s.createMilestone))
	mux.Handle("PATCH /api/v1/milestones/{milestoneId}", a(s.updateMilestone))
	mux.Handle("DELETE /api/v1/milestones/{milestoneId}", a(s.deleteMilestone))
	mux.Handle("GET /api/v1/projects/{projectId}/access", a(s.projectAccessList))
	mux.Handle("PUT /api/v1/projects/{projectId}/access/{userId}", a(s.setProjectAccess))
	mux.Handle("DELETE /api/v1/projects/{projectId}/access/{userId}", a(s.removeProjectAccess))
	mux.Handle("POST /api/v1/organizations", a(s.createOrganization))
	mux.Handle("PATCH /api/v1/organizations/{orgId}", a(s.updateOrganization))
	mux.Handle("POST /api/v1/organizations/{orgId}/invitations", a(s.inviteMember))
	mux.Handle("POST /api/v1/organizations/{orgId}/invitations/accept", a(s.acceptInvitation))
	mux.Handle("GET /api/v1/me/invitations", a(s.myInvitations))
	mux.Handle("PUT /api/v1/organizations/{orgId}/members/{userId}/roles", a(s.setMemberRoles))
	mux.Handle("PATCH /api/v1/organizations/{orgId}/members/{userId}", a(s.setMemberState))
	mux.Handle("POST /api/v1/organizations/{orgId}/ai/ask", a(s.askAI))
	mux.Handle("GET /api/v1/organizations/{orgId}/ai/history", a(s.aiHistory))
	mux.Handle("GET /api/v1/organizations/{orgId}/ai/policy", a(s.getAIPolicy))
	mux.Handle("PUT /api/v1/organizations/{orgId}/ai/policy", a(s.updateAIPolicy))
	mux.Handle("GET /api/v1/organizations/{orgId}/hiring", a(s.listHiring))
	mux.Handle("POST /api/v1/organizations/{orgId}/requisitions", a(s.createRequisition))
	mux.Handle("PATCH /api/v1/requisitions/{reqId}", a(s.updateRequisition))
	mux.Handle("POST /api/v1/requisitions/{reqId}/candidates", a(s.createCandidate))
	mux.Handle("PATCH /api/v1/candidates/{candidateId}", a(s.updateCandidate))
	mux.Handle("GET /api/v1/organizations/{orgId}/goals", a(s.listGoals))
	mux.Handle("POST /api/v1/organizations/{orgId}/goals", a(s.createGoal))
	mux.Handle("PATCH /api/v1/goals/{goalId}", a(s.updateGoal))
	mux.Handle("DELETE /api/v1/goals/{goalId}", a(s.deleteGoal))
	mux.Handle("GET /api/v1/organizations/{orgId}/threads", a(s.listThreads))
	mux.Handle("POST /api/v1/organizations/{orgId}/threads", a(s.createThread))
	mux.Handle("GET /api/v1/threads/{threadId}", a(s.getThread))
	mux.Handle("POST /api/v1/threads/{threadId}/messages", a(s.postMessage))
	mux.Handle("PATCH /api/v1/threads/{threadId}/state", a(s.setThreadState))
	mux.Handle("GET /api/v1/organizations/{orgId}/executive-summary", a(s.executiveSummary))
	mux.Handle("GET /api/v1/organizations/{orgId}/reports/tasks", a(s.reportTasks))
	mux.Handle("GET /api/v1/organizations/{orgId}/reports/tasks.csv", a(s.exportTasks))
	mux.Handle("GET /api/v1/organizations/{orgId}/reports/exports", a(s.listReportExports))
	mux.Handle("GET /api/v1/organizations/{orgId}/report-views", a(s.listReportViews))
	mux.Handle("POST /api/v1/organizations/{orgId}/report-views", a(s.createReportView))
	mux.Handle("DELETE /api/v1/report-views/{viewId}", a(s.deleteReportView))
	mux.Handle("GET /api/v1/organizations/{orgId}/audit-events", a(s.auditEvents))

	return s.middleware(mux)
}

type ctxKey int

const (
	requestIDKey ctxKey = iota
	actorKey
)

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.now()
		rid := r.Header.Get("X-Request-Id")
		if rid == "" || len(rid) > 64 {
			rid = uuid.NewString()
		}
		ctx := context.WithValue(r.Context(), requestIDKey, rid)
		r = r.WithContext(ctx)
		h := w.Header()
		h.Set("X-Request-Id", rid)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer")
		limit := int64(1 << 20)
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/attachments") {
			limit = maxAttachmentBytes + 64<<10
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)

		rec := &statusRecorder{ResponseWriter: w, status: 200}
		defer func() {
			if p := recover(); p != nil {
				s.log.Error("panic", "request_id", rid, "panic", p, "stack", string(debug.Stack()))
				writeError(rec, r, http.StatusInternalServerError, "INTERNAL", "Something went wrong.", nil)
			}
			s.log.Info("request", "request_id", rid, "method", r.Method, "path", r.URL.Path,
				"status", rec.status, "duration_ms", s.now().Sub(start).Milliseconds())
		}()
		next.ServeHTTP(rec, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// ---------------------------------------------------------------------------
// JSON helpers and the uniform error contract.
// ---------------------------------------------------------------------------

type apiError struct {
	Code        string            `json:"code"`
	Message     string            `json:"message"`
	RequestID   string            `json:"request_id"`
	FieldErrors map[string]string `json:"field_errors,omitempty"`
}

// httpError lets handlers return typed failures from inside transactions.
type httpError struct {
	status int
	code   string
	msg    string
	fields map[string]string
}

func (e *httpError) Error() string { return e.msg }

func errNotFound() error {
	return &httpError{404, "NOT_FOUND", "The resource does not exist or you do not have access.", nil}
}
func errForbidden(msg string) error {
	return &httpError{403, "FORBIDDEN", msg, nil}
}
func errValidation(fields map[string]string) error {
	return &httpError{422, "VALIDATION_FAILED", "Some fields need attention.", fields}
}
func errUnprocessable(code, msg string) error { return &httpError{422, code, msg, nil} }

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, msg string, fields map[string]string) {
	writeJSON(w, r, status, apiError{Code: code, Message: msg, RequestID: requestID(r.Context()), FieldErrors: fields})
}

// fail maps any handler error to the uniform contract without leaking internals.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var he *httpError
	if errors.As(err, &he) {
		writeError(w, r, he.status, he.code, he.msg, he.fields)
		return
	}
	if store.IsNoRows(err) {
		writeError(w, r, 404, "NOT_FOUND", "The resource does not exist or you do not have access.", nil)
		return
	}
	if msg, ok := store.ConstraintMessage(err); ok {
		writeError(w, r, 422, "CONSTRAINT_VIOLATION", msg, nil)
		return
	}
	s.log.Error("request failed", "request_id", requestID(r.Context()), "error", err)
	writeError(w, r, 500, "INTERNAL", "Something went wrong.", nil)
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &httpError{400, "BAD_REQUEST", "The request body is not valid JSON for this endpoint.", nil}
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return &httpError{400, "BAD_REQUEST", "The request body must contain a single JSON object.", nil}
	}
	return nil
}

func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, errNotFound()
	}
	return id, nil
}

// ifMatchVersion parses If-Match: "3" (or W/"3"). Missing header => 428.
func ifMatchVersion(r *http.Request) (int, error) {
	v := strings.TrimSpace(r.Header.Get("If-Match"))
	if v == "" {
		return 0, &httpError{428, "PRECONDITION_REQUIRED", "Send If-Match with the resource version to update it.", nil}
	}
	n, err := strconv.Atoi(strings.Trim(strings.TrimPrefix(v, "W/"), `"`))
	if err != nil || n < 1 {
		return 0, &httpError{400, "BAD_REQUEST", "If-Match must be a resource version.", nil}
	}
	return n, nil
}

func errVersionConflict() error {
	return &httpError{412, "VERSION_CONFLICT", "This record changed since you loaded it. Refresh and try again.", nil}
}

func setETag(w http.ResponseWriter, version int) {
	w.Header().Set("ETag", `"`+strconv.Itoa(version)+`"`)
}
