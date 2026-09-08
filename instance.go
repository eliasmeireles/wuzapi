package main

import (
	"database/sql"
	"flag"
	"os"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/rs/zerolog/log"
)

// A wuzapi deployment can be split into several instances that share one
// database. Each process owns only the users whose `instance` column matches
// its own name and refuses every request for the others, so two deployments —
// say one subscribed to Message events and one that only sends — can run over
// the same users table and the same whatsmeow device store.
//
// Sharing the store is the whole point: moving a session from one pool to the
// other is an UPDATE of one column plus a reconnect, not a new QR pairing.
//
// An empty instance name (the default, and what every unpatched deployment
// has) keeps the original behaviour: the process owns every user, whatever the
// column says. That is what makes this patch a no-op until a name is set.
var instanceName = flag.String("instance", "", "Name of this wuzapi instance when several share one database (env WUZAPI_INSTANCE)")

// instanceIsDefault marks the one instance that also owns the users created
// before the split, whose instance column is still empty. Exactly one instance
// of a pool may carry it: with two default instances both would connect the
// same unassigned device and WhatsApp would answer with StreamReplaced, each
// process kicking the other in a loop.
var instanceIsDefault = flag.Bool("instancedefault", false, "This instance also owns users with no instance recorded (env WUZAPI_INSTANCE_DEFAULT)")

// instanceOwnership caches the users.instance value by token. Ownership is
// checked on every authenticated request, and a stale entry would either
// serve a session this process no longer owns or reject one it just took
// over, so the TTL is deliberately short instead of the unbounded caching
// userinfocache does for the rest of the user row.
var instanceOwnership = cache.New(5*time.Second, 30*time.Second)

// currentInstance returns this process's instance name, preferring the flag
// over WUZAPI_INSTANCE.
func currentInstance() string {
	if instanceName != nil && *instanceName != "" {
		return strings.TrimSpace(*instanceName)
	}
	return strings.TrimSpace(os.Getenv("WUZAPI_INSTANCE"))
}

// shardingEnabled reports whether this process is one instance among several.
func shardingEnabled() bool {
	return currentInstance() != ""
}

// isDefaultInstance reports whether this process also owns the users with no
// instance recorded.
func isDefaultInstance() bool {
	if instanceIsDefault != nil && *instanceIsDefault {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WUZAPI_INSTANCE_DEFAULT"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// ownsInstance reports whether a user carrying the given instance value
// belongs to this process. With sharding off every user is owned. With
// sharding on, a user is owned when the names match — and a user that was
// never assigned (empty column: created before the split) belongs to the
// instance flagged as the default, so exactly one process picks it up.
func ownsInstance(userInstance string) bool {
	if !shardingEnabled() {
		return true
	}
	userInstance = strings.TrimSpace(userInstance)
	if userInstance == "" {
		return isDefaultInstance()
	}
	return userInstance == currentInstance()
}

// instanceOfToken reads the instance a token's user is assigned to, going
// through a short-lived cache. A missing user yields "" — authalice has
// already rejected that case.
func (s *server) instanceOfToken(token string) string {
	if cached, found := instanceOwnership.Get(token); found {
		return cached.(string)
	}
	var owner sql.NullString
	err := s.db.QueryRow("SELECT instance FROM users WHERE token=$1 LIMIT 1", token).Scan(&owner)
	if err != nil && err != sql.ErrNoRows {
		log.Warn().Err(err).Msg("Could not read instance ownership, assuming unassigned")
	}
	value := strings.TrimSpace(owner.String)
	instanceOwnership.Set(token, value, cache.DefaultExpiration)
	return value
}

// forgetInstanceOwnership drops a token's cached ownership so a reassignment
// takes effect on the next request instead of after the TTL.
func forgetInstanceOwnership(token string) {
	instanceOwnership.Delete(token)
}
