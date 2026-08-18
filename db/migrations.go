package db

import (
	"github.com/lightningnetwork/lnd/sqldb/v2"
)

const (
	// LatestMigrationVersion is the latest migration version of the
	// database. This is used to implement downgrade protection for the
	// daemon.
	//
	// NOTE: This MUST be updated when a new migration is added.
	LatestMigrationVersion = 2
)

// MakeMigrationDescriptors returns the ordered migration descriptors for
// faraday's SQL schema. Both migrations are schema-only, so neither carries a
// programmatic migration step.
func MakeMigrationDescriptors() []sqldb.MigrationDescriptor {
	return []sqldb.MigrationDescriptor{
		{
			Name:          "chanevents",
			Version:       1,
			SchemaVersion: 1,
		},
		{
			Name:          "chanevents_ts_idx",
			Version:       2,
			SchemaVersion: 2,
		},
	}
}
