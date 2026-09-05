package finance

import "errors"

// ErrNotFound means the instrument or rate does not exist upstream, as opposed
// to a request that failed.
var ErrNotFound = errors.New("not found")

// ErrRateLimited means a provider refused the request because the caller has
// exceeded its quota. It is retryable later but not immediately.
var ErrRateLimited = errors.New("provider rate limited")

// ErrInsufficientHistory means the instruments given to Align share too few
// trading dates to produce the requested number of observations.
var ErrInsufficientHistory = errors.New("insufficient overlapping history")
