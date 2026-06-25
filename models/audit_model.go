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

// nullableInt64 converts a zero ID (meaning "none") to SQL NULL, since 0 is
// never a valid admin/user/room/booking primary key.
func nullableInt64(id int64) interface{} {
	if id == 0 {
		return nil
	}
	return id
}

// List returns audit entries newest-first, narrowed by filter, capped at 500
// rows — the UI's date-range filter is the intended way to look further back
// than that without overwhelming a single response.
func (m *AuditModel) List(filter AuditFilter) ([]AuditLog, error) {
	query := `
		SELECT id, actor_type, actor_label, action, target_type, target_label, details, ip_address, user_agent,
		       TO_CHAR(created_at, 'YYYY-MM-DD HH24:MI:SS')
		FROM audit_logs
		WHERE 1=1
	`
	args := []any{}

	if filter.ActorLabel != "" {
		args = append(args, "%"+strings.ToLower(filter.ActorLabel)+"%")
		p := fmt.Sprintf("$%d", len(args))
		query += " AND (LOWER(actor_label) LIKE " + p + " OR LOWER(target_label) LIKE " + p + ")"
	}
	if filter.Action != "" {
		args = append(args, filter.Action)
		query += fmt.Sprintf(" AND action = $%d", len(args))
	}
	if filter.From != "" {
		args = append(args, filter.From)
		query += fmt.Sprintf(" AND created_at >= $%d::date", len(args))
	}
	if filter.To != "" {
		args = append(args, filter.To)
		query += fmt.Sprintf(" AND created_at < ($%d::date + INTERVAL '1 day')", len(args))
	}

	query += " ORDER BY created_at DESC LIMIT 500"

	rows, err := m.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []AuditLog{}
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.ID, &l.ActorType, &l.ActorLabel, &l.Action, &l.TargetType, &l.TargetLabel, &l.Details, &l.IPAddress, &l.UserAgent, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}
