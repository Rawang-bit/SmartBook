package controllers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"bookroom-management-system/models"
)

// ListRooms returns all rooms. Public endpoint — no login required.
func (c *Controller) ListRooms(w http.ResponseWriter, r *http.Request) {
	rows, err := c.DB.Query(`
		SELECT id, name, capacity, location, status
		FROM rooms
		ORDER BY id ASC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	rooms := []models.Room{}
	for rows.Next() {
		var room models.Room
		if err := rows.Scan(&room.ID, &room.Name, &room.Capacity, &room.Location, &room.Status); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rooms = append(rooms, room)
	}

	// Check for errors that occurred while iterating rows
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, rooms)
}

// CreateRoom adds a new room. Admin only.
func (c *Controller) CreateRoom(w http.ResponseWriter, r *http.Request) {
	var req models.RoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	room, err := c.saveRoom(0, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, room)
}

// UpdateRoom edits an existing room. Admin only.
func (c *Controller) UpdateRoom(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/rooms/")
	if !ok {
		return
	}

	var req models.RoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	room, err := c.saveRoom(id, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, room)
}

// saveRoom inserts (id == 0) or updates (id > 0) a room after validating input.
func (c *Controller) saveRoom(id int64, req models.RoomRequest) (models.Room, error) {
	req.Name     = strings.TrimSpace(req.Name)
	req.Location = strings.TrimSpace(req.Location)
	req.Status   = normalizeRoomStatus(req.Status)

	if req.Name == "" || req.Location == "" || req.Capacity < 1 {
		return models.Room{}, errors.New("room name, location, and capacity are required")
	}

	var room models.Room
	var err  error

	if id == 0 {
		// Insert a new room row
		err = c.DB.QueryRow(`
			INSERT INTO rooms(name, capacity, location, status)
			VALUES($1, $2, $3, $4)
			RETURNING id, name, capacity, location, status
		`, req.Name, req.Capacity, req.Location, req.Status).Scan(
			&room.ID, &room.Name, &room.Capacity, &room.Location, &room.Status,
		)
	} else {
		// Update the existing room row
		err = c.DB.QueryRow(`
			UPDATE rooms
			SET name = $1, capacity = $2, location = $3, status = $4
			WHERE id = $5
			RETURNING id, name, capacity, location, status
		`, req.Name, req.Capacity, req.Location, req.Status, id).Scan(
			&room.ID, &room.Name, &room.Capacity, &room.Location, &room.Status,
		)
	}

	if errors.Is(err, sql.ErrNoRows) {
		return room, errors.New("room not found")
	}
	if err != nil {
		if isUniqueViolation(err) {
			return room, errors.New("room name already exists")
		}
		return room, err
	}

	return room, nil
}

// DeleteRoom permanently removes a room.
// The database blocks this if the room has any bookings attached to it.
func (c *Controller) DeleteRoom(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/rooms/")
	if !ok {
		return
	}

	result, err := c.DB.Exec(`DELETE FROM rooms WHERE id = $1`, id)
	if err != nil {
		if isForeignKeyViolation(err) {
			writeError(w, http.StatusBadRequest, "cannot delete a room that has bookings")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	count, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count == 0 {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
