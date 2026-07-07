package models

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
)

// AuditModel records and retrieves the audit trail; entries are append-only — no Update or Delete method exists.
type AuditModel struct {
	DB *sql.DB
}

// AuditPageSize is rows per page; every entry is reachable via pagination (no hard cap on total rows).
const AuditPageSize = 50

// Record writes one audit entry; failures are logged but never returned — audit errors must not block the action.
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

// nullableInt64 returns nil for a zero ID so foreign-key columns store NULL rather than 0.
func nullableInt64(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// auditWhereClause builds the shared WHERE clause and args for List so count and fetch queries cannot drift.
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

// List returns audit entries newest-first; Page>=1 paginates (AuditPageSize rows), Page<=0 returns all rows for export.
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
