package models

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
)

// AuditModel records and retrieves the audit trail. Entries are append-only —
// there is deliberately no Update or Delete method, and no controller exposes
// one, so the trail can never be edited or erased through the application.
type AuditModel struct {
	DB *sql.DB
}

// AuditPageSize is how many rows the paginated audit-trail view shows per
// page. There is no cap on how many total rows are reachable across pages —
// unlike a hard row limit, pagination keeps every entry in the table viewable.
const AuditPageSize = 50

// Record writes one audit entry. Failures are logged but never returned to
// the caller — exactly like email delivery elsewhere in this app, a logging
// problem must never block or roll back the action it's describing.
func (m *AuditModel) Record(e AuditEntry) {
	actorID := nullableInt64(e.ActorID)
	targetID := nullableInt64(e.TargetID)

	_, err := m.DB.Exec(`
		INSERT INTO audit_logs(actor_type, actor_id, actor_label, action, target_type, target_id, target_label, details, ip_address, user_agent)
		VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, e.ActorType, actorID, e.ActorLabel, e.Action, e.TargetType, targetID, e.TargetLabel, e.Details, e.IPAddress, e.UserAgent)
	if err != nil {
		log.Printf("[AUDIT] failed to record action %q: %v", e.Action, err)
	}
}

// nullableInt64 returns nil for a zero ID so optional foreign-key columns
// store NULL instead of 0 (0 is never a valid primary key in this schema).
func nullableInt64(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// auditWhereClause builds the WHERE clause and args shared by both the count
// and the row-fetch queries in List, so the two can never drift apart.
func auditWhereClause(filter AuditFilter) (string, []any) {
	clause := "WHERE 1=1"
	args := []any{}

	if filter.ActorLabel != "" {
		args = append(args, "%"+strings.ToLower(filter.ActorLabel)+"%")
		p := fmt.Sprintf("$%d", len(args))
		clause += " AND (LOWER(actor_label) LIKE " + p + " OR LOWER(target_label) LIKE " + p + ")"
	}
	if filter.Action != "" {
		args = append(args, filter.Action)
		clause += fmt.Sprintf(" AND action = $%d", len(args))
	}
	if filter.From != "" {
		args = append(args, filter.From)
		clause += fmt.Sprintf(" AND created_at >= $%d::date", len(args))
	}
	if filter.To != "" {
		args = append(args, filter.To)
		clause += fmt.Sprintf(" AND created_at < ($%d::date + INTERVAL '1 day')", len(args))
	}

	return clause, args
}

// List returns audit entries newest-first, narrowed by filter.
// filter.Page >= 1 returns that page, AuditPageSize rows at a time, with
// Total/TotalPages filled in so the caller can render pagination controls.
// filter.Page <= 0 returns every matching row in one result, with no LIMIT —
// used only for exporting the full filtered trail (see ListAuditLogs), never
// for the on-screen paginated list.
func (m *AuditModel) List(filter AuditFilter) (AuditPage, error) {
	whereClause, args := auditWhereClause(filter)

	var total int
	if err := m.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs `+whereClause, args...).Scan(&total); err != nil {
		return AuditPage{}, err
	}

	query := `
		SELECT id, actor_type, actor_label, action, target_type, target_label, details, ip_address, user_agent,
		       TO_CHAR(created_at, 'YYYY-MM-DD HH24:MI:SS')
		FROM audit_logs ` + whereClause + ` ORDER BY created_at DESC`

	page := filter.Page
	if page >= 1 {
		offset := (page - 1) * AuditPageSize
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", AuditPageSize, offset)
	} else {
		page = 1 // reported back as page 1 of 1 when returning everything unpaginated
	}

	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return AuditPage{}, err
	}
	defer rows.Close()

	logs := []AuditLog{}
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.ID, &l.ActorType, &l.ActorLabel, &l.Action, &l.TargetType, &l.TargetLabel, &l.Details, &l.IPAddress, &l.UserAgent, &l.CreatedAt); err != nil {
			return AuditPage{}, err
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return AuditPage{}, err
	}

	totalPages := (total + AuditPageSize - 1) / AuditPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if filter.Page < 1 {
		totalPages = 1
	}

	return AuditPage{
		Logs:       logs,
		Total:      total,
		Page:       page,
		PageSize:   AuditPageSize,
		TotalPages: totalPages,
	}, nil
}
