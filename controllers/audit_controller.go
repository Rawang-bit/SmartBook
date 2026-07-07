package controllers

import (
	"net/http"
	"strconv"

	"bookroom-management-system/models"
)

// ListAuditLogs returns the audit trail filtered by actor, action, or date range.
// ?page=N paginates (50 rows, default 1); ?all=1 returns every row (export use only).
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
