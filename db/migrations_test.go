package db

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLatestMigrationVersion ensures that LatestMigrationVersion stays in sync
// with the highest-numbered .up.sql file in the migrations directory. Each
// migration — whether pure SQL or programmatic (with a dummy SQL file) — gets
// its own numbered file pair, so the max file number must equal the constant.
func TestLatestMigrationVersion(t *testing.T) {
	entries, err := sqlSchemas.ReadDir("sqlc/migrations")
	require.NoError(t, err)

	var maxVersion uint
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}

		parts := strings.SplitN(entry.Name(), "_", 2)
		require.NotEmpty(t, parts)

		v, err := strconv.ParseUint(parts[0], 10, 64)
		require.NoError(t, err)

		if uint(v) > maxVersion {
			maxVersion = uint(v)
		}
	}

	require.EqualValues(
		t, maxVersion, LatestMigrationVersion,
		"LatestMigrationVersion is out of date, update "+
			"db/migrations.go",
	)
}
