package dump

import "fmt"

// LimitError is returned when a subset limit is exceeded.
type LimitError struct {
	Limit   string
	Message string
}

func (e *LimitError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("subset limit exceeded: %s", e.Limit)
}

func newLimitError(limit, format string, args ...any) error {
	return &LimitError{
		Limit:   limit,
		Message: fmt.Sprintf("subset %s limit: "+format, append([]any{limit}, args...)...),
	}
}
