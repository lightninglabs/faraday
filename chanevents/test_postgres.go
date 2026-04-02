//go:build test_db_postgres

package chanevents

import (
	"testing"

	"github.com/lightninglabs/faraday/db"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/stretchr/testify/require"
)

// NewTestDB creates a new test chanevents.Store backed by a postgres DB.
func NewTestDB(t *testing.T, clock clock.Clock) *Store {
	// We'll create a new test database. The call to NewTestPostgresDB will
	// automatically create the DB and apply the migrations.
	testDB := db.NewTestPostgresDB(t)

	// Now, we'll create the FaradayDB instance from the test database. The
	// FaradayDB is the main database object that holds the connection and
	// the generated querier.
	faradayDB := createStore(t, testDB.BaseDB, clock)

	t.Cleanup(func() {
		require.NoError(t, faradayDB.Close())
	})

	return faradayDB
}
