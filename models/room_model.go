package models

import (
	"database/sql"
	"fmt"
	"strings"

	"bookroom-management-system/utils"
)

// RoomModel manages all database operations and validation for meeting rooms.
// It is the single source of truth for room availability and configuration.
type RoomModel struct {
	DB *sql.DB
}

// List returns all rooms ordered by ID.
func (m *RoomModel) List() ([]Room, error) {
	rows, err := m.DB.Query(`
		SELECT id, name, capacity, location, status FROM rooms ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rooms := []Room{}
	for rows.Next() {
		var room Room
		if err := rows.Scan(&room.ID, &room.Name, &room.Capacity, &room.Location, &room.Status); err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
	}
	return rooms, rows.Err()
}

// GetByID fetches a single room by its primary key.
// Returns ErrNotFound if no room has that ID.
func (m *RoomModel) GetByID(id int64) (Room, error) {
	var room Room
	err := m.DB.QueryRow(`
		SELECT id, name, capacity, location, status FROM rooms WHERE id = $1
	`, id).Scan(&room.ID, &room.Name, &room.Capacity, &room.Location, &room.Status)
	if err == sql.ErrNoRows {
		return Room{}, ErrNotFound
	}
	return room, err
}

// GetByName fetches a single room by its unique name.
// Returns ErrNotFound if no room has that name.
func (m *RoomModel) GetByName(name string) (Room, error) {
	var room Room
	err := m.DB.QueryRow(`
		SELECT id, name, capacity, location, status FROM rooms WHERE name = $1
	`, name).Scan(&room.ID, &room.Name, &room.Capacity, &room.Location, &room.Status)
	if err == sql.ErrNoRows {
		return Room{}, ErrNotFound
	}
	return room, err
}

// Save inserts (id == 0) or updates (id > 0) a room after validating all required fields.
// Returns ErrDuplicate if the name is taken, ErrNotFound on update with an unknown ID.
func (m *RoomModel) Save(id int64, req RoomRequest) (Room, error) {
	req.Name     = strings.TrimSpace(req.Name)
	req.Location = strings.TrimSpace(req.Location)
	req.Status   = utils.NormalizeRoomStatus(req.Status)

	if req.Name == "" || req.Location == "" || req.Capacity < 1 {
		return Room{}, fmt.Errorf("room name, location, and capacity are required")
	}

	var room Room
	var err  error

	if id == 0 {
		err = m.DB.QueryRow(`
			INSERT INTO rooms(name, capacity, location, status)
			VALUES($1, $2, $3, $4)
			RETURNING id, name, capacity, location, status
		`, req.Name, req.Capacity, req.Location, req.Status).Scan(
			&room.ID, &room.Name, &room.Capacity, &room.Location, &room.Status,
		)
	} else {
		err = m.DB.QueryRow(`
			UPDATE rooms
			SET name = $1, capacity = $2, location = $3, status = $4
			WHERE id = $5
			RETURNING id, name, capacity, location, status
		`, req.Name, req.Capacity, req.Location, req.Status, id).Scan(
			&room.ID, &room.Name, &room.Capacity, &room.Location, &room.Status,
		)
	}

	if err == sql.ErrNoRows {
		return Room{}, ErrNotFound
	}
	if err != nil {
		if utils.IsUniqueViolation(err) {
			return Room{}, ErrDuplicate
		}
		return Room{}, err
	}
	return room, nil
}

// Delete permanently removes a room.
// Returns ErrForeignKey if the room still has bookings, ErrNotFound if the ID is unknown.
func (m *RoomModel) Delete(id int64) error {
	result, err := m.DB.Exec(`DELETE FROM rooms WHERE id = $1`, id)
	if err != nil {
		if utils.IsForeignKeyViolation(err) {
			return ErrForeignKey
		}
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
