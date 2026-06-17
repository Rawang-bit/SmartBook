package controllers

import (
	"errors"
	"net/http"

	"bookroom-management-system/models"
)

// ListRooms returns all rooms. Public endpoint — no login required.
func (c *Controller) ListRooms(w http.ResponseWriter, r *http.Request) {
	rooms, err := c.Rooms.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rooms)
}

// CreateRoom adds a new room. Super admin only.
func (c *Controller) CreateRoom(w http.ResponseWriter, r *http.Request) {
	var req models.RoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	room, err := c.Rooms.Save(0, req)
	if errors.Is(err, models.ErrDuplicate) {
		writeError(w, http.StatusBadRequest, "room name already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, room)
}

// UpdateRoom edits an existing room. Super admin only.
func (c *Controller) UpdateRoom(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/rooms/")
	if !ok {
		return
	}

	var req models.RoomRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	room, err := c.Rooms.Save(id, req)
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if errors.Is(err, models.ErrDuplicate) {
		writeError(w, http.StatusBadRequest, "room name already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, room)
}

// DeleteRoom permanently removes a room. Super admin only.
// The database blocks this if the room has any bookings attached to it.
func (c *Controller) DeleteRoom(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/rooms/")
	if !ok {
		return
	}

	err := c.Rooms.Delete(id)
	if errors.Is(err, models.ErrForeignKey) {
		writeError(w, http.StatusBadRequest, "cannot delete a room that has bookings")
		return
	}
	if errors.Is(err, models.ErrNotFound) {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
