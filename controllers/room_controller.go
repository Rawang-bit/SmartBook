package controllers

import (
	"errors"
	"log"
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

// CreateRoom adds a new room. General admin only — room management is an
// operational task, so super_admin is deliberately excluded.
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

	c.audit(r, "room_created", "room", room.Name, room.ID, "")

	writeJSON(w, http.StatusCreated, room)
}

// UpdateRoom edits an existing room. General admin only — see CreateRoom.
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

	c.audit(r, "room_updated", "room", room.Name, room.ID, "")

	writeJSON(w, http.StatusOK, room)
}

// DeleteRoom permanently removes a room. General admin only — see CreateRoom.
// The database blocks this if the room has any bookings attached to it.
func (c *Controller) DeleteRoom(w http.ResponseWriter, r *http.Request) {
	id, ok := idFromPath(w, r, "/api/rooms/")
	if !ok {
		return
	}

	room, getErr := c.Rooms.GetByID(id)
	if getErr != nil {
		log.Printf("[ROOM] GetByID(%d) before delete failed — audit label will be empty: %v", id, getErr)
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

	c.audit(r, "room_deleted", "room", room.Name, id, "")

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
