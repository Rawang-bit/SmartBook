package controllers

import (
	"net/http"

	"bookroom-management-system/models"
)

// ListAuditLogs returns the audit trail, optionally filtered by actor/target
// label, action, or date range. Super admin only — audit logs are read-only
// for everyone, and not even visible to general admins or normal users.
func (c *Controller) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	filter := models.AuditFilter{
		ActorLabel: r.URL.Query().Get("actor"),
		Action:     r.URL.Query().Get("action"),
		From:       r.URL.Query().Get("from"),
		To:         r.URL.Query().Get("to"),
	}

	logs, err := c.Audit.List(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch audit logs")
		return
	}

	writeJSON(w, http.StatusOK, logs)
}
