package chanevents

import (
	"testing"

	"github.com/lightninglabs/faraday/db/sqlc"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/sqldb/v2"
)

// createStore is a helper function that creates a new Store.
func createStore(t *testing.T, sqlDB *sqldb.BaseDB, clock clock.Clock) *Store {
	queries := sqlc.NewForType(sqlDB, sqlDB.BackendType)

	store := NewStore(sqlDB, queries, clock)

	return store
}
