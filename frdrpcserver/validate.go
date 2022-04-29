package frdrpcserver

import (
	"fmt"
	"time"
)

// validateTimes checks that a start time is before an end time, progresses
// end time to the present if it is not set and returns start and end times.
func validateTimes(startTime, endTime uint64) (time.Time, time.Time, error) {
	start := time.Unix(int64(startTime), 0)
	end := time.Unix(int64(endTime), 0)

	// If end time is not set, progress it to the present.
	if endTime == 0 {
		end = time.Now()
	}

	if start.After(end) {
		return start, end, fmt.Errorf("start time: %v after "+
			"end: %v", start, end)
	}

	return start, end, nil
}
