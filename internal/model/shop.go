package model

import "time"

// RetryWindow bounds new exchange attempts, not requests already in flight.
func (c ShopConfig) RetryWindow() time.Duration {
	return time.Duration(max(0, min(120, c.RetrySeconds)) * float64(time.Second))
}

// Zero means one request, with a short dispatch grace period, not infinite retries.
func (c ShopConfig) DispatchWindow() time.Duration {
	if window := c.RetryWindow(); window > 0 {
		return window
	}
	return time.Minute
}
