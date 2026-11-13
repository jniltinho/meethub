package jobs

import "time"

// RetryDelayForTest exposes the backoff for a runner with the given base and cap.
func RetryDelayForTest(base, capDelay time.Duration, n int) time.Duration {
	r := &Runner{opts: Options{RetryBase: base, RetryCap: capDelay}}
	return r.retryDelay(n, nil, nil)
}
