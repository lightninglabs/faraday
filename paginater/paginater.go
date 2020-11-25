package paginater

import "context"

// PaginatedQuery is a function which makes a call to a paginated api and adds
// returns the index offset of the last entry and the number of events that were
// returned.
type PaginatedQuery func(offset, maxEvents uint64) (uint64, uint64, error)

// QueryPaginated gets calls the paginated query function until it it has
// retrieved all the items from the query endpoint, or the calling context is
// cancelled. Note that the query function is responsible for collecting the
// items returned by the API (if required) so that the pagination logic can
// remain generic.
func QueryPaginated(ctx context.Context, query PaginatedQuery, offset,
	maxEvents uint64) error {

	for {
		// Make a query to the paginated API.
		newOffset, numItems, err := query(offset, maxEvents)
		if err != nil {
			return err
		}

		// If we have less than the maximum number of items, we do not
		// need to query further for more items.
		if numItems < maxEvents {
			return nil
		}

		// Update the offset for the next query.
		offset = newOffset

		// Each time we loop around, check whether we have any errors on
		// our context. This will occur if our context is cancelled, and
		// allows us an opportunity to exit early.
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}
