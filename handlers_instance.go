package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// SetUserInstance moves a user to another wuzapi instance sharing this
// database. The device credentials live in the shared whatsmeow store, so the
// move costs a reconnect on the target instance — never a new QR pairing.
//
// Call it on the instance that currently holds the session: it is the only
// process that can drop the live socket, and leaving that socket open would
// mean two processes speaking for the same device.
func (s *server) SetUserInstance() http.HandlerFunc {
	type request struct {
		Instance string `json:"instance"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		userID := mux.Vars(r)["id"]

		var body request
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.Respond(w, r, http.StatusBadRequest, errors.New("could not decode payload"))
			return
		}
		target := strings.TrimSpace(body.Instance)

		var token string
		var previous sql.NullString
		err := s.db.QueryRow("SELECT token, COALESCE(instance, '') FROM users WHERE id=$1", userID).Scan(&token, &previous)
		if err == sql.ErrNoRows {
			s.Respond(w, r, http.StatusNotFound, errors.New("user not found"))
			return
		}
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
			return
		}

		if _, err := s.db.Exec("UPDATE users SET instance=$1 WHERE id=$2", target, userID); err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
			return
		}
		forgetInstanceOwnership(token)

		// Drop the socket if this process is handing the session over. The
		// kill path also clears users.connected, so the target instance only
		// reconnects when asked to — no two instances racing for the device.
		released := false
		if !ownsInstance(target) && clientManager.GetWhatsmeowClient(userID) != nil {
			signalKill(userID)
			released = true
		}
		userinfocache.Delete(token)

		log.Info().
			Str("userID", userID).
			Str("from", previous.String).
			Str("to", target).
			Bool("released", released).
			Msg("User reassigned to another wuzapi instance")

		response, err := json.Marshal(map[string]interface{}{
			"id":       userID,
			"instance": target,
			"previous": previous.String,
			"released": released,
			"details":  "Instance updated",
		})
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
			return
		}
		s.Respond(w, r, http.StatusOK, string(response))
	}
}
