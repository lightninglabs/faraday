package db

import (
	"testing"

	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/lightningnetwork/lnd/sqldb/v2"
)

// NewTestPostgresDB is a helper function that creates a Postgres database for
// testing.
func NewTestPostgresDB(t *testing.T) *sqldb.PostgresStore {
	t.Helper()

	t.Logf("Creating new Postgres DB for testing")

	sqlFixture := sqldb.NewTestPgFixture(
		t, sqldb.DefaultPostgresFixtureLifetime,
	)
	t.Cleanup(func() {
		sqlFixture.TearDown(t)
	})

	return sqldb.NewTestPostgresDB(t, sqlFixture, FaradayMigrationSets)
}
