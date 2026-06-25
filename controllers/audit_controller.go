package controllers

import (
	"net/http"
	"strconv"

	"bookroom-management-system/models"
)

// ListAuditLogs returns the audit trail, optionally filtered by actor/target
// label, action, or date range. Super admin only — audit logs are read-only
// for everyone, and not even visible to general admins or normal users.
//
// ?page=N returns that page (50 rows each, see AuditModel.AuditPageSize),
// defaulting to 1. ?all=1 instead returns every matching row in one
// response with no pagination at all — used only for exporting the full
// filtered trail, never for the on-screen list.
func (c *Controller) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	page := 1
	if r.URL.Query().Get("all") == "1" {
		page = 0
	} else if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p >= 1 {
		page = p
	}

	filter := models.AuditFilter{
		ActorLabel: r.URL.Query().Get("actor"),
		Action:     r.URL.Query().Get("action"),
		From:       r.URL.Query().Get("from"),
		To:         r.URL.Query().Get("to"),
		Page:       page,
	}

	result, err := c.Audit.List(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch audit logs")
		return
	}

	writeJSON(w, http.StatusOK, result)
}
