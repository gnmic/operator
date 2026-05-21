package http

import (
	"fmt"
	"net/url"
)

// extractNextPageInfo extracts pagination information from a response
func (l *Loader) extractNextPageInfo(raw interface{}) (string, error) {
	if l.spec.Pagination == nil || l.spec.Pagination.NextField == "" {
		return "", nil
	}

	// Only objects can have "next" fields
	obj, ok := raw.(map[string]interface{})
	if !ok {
		// array case -> no pagination
		return "", nil
	}

	val, ok := obj[l.spec.Pagination.NextField]
	if val == nil {
		// No next page -> end of pagination
		return "", nil
	}
	if !ok {
		return "", fmt.Errorf("nextField '%s' not found in response", l.spec.Pagination.NextField)
	}

	next, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("nextField '%s' is not a string in response", l.spec.Pagination.NextField)
	}

	return next, nil
}

// buildNextURL constructs the URL for the next page based on the current URL and pagination info
func (l *Loader) buildNextURL(currentURL, nextVal string) (string, error) {
	// nextVal is a full URL -> return as is
	if parsed, err := url.Parse(nextVal); err == nil && parsed.Scheme != "" {
		return nextVal, nil
	}

	// nextVal is a token -> append as query parameter
	parsedURL, err := url.Parse(currentURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse current URL in order to build next URL: %w", err)
	}
	q := parsedURL.Query()
	q.Set(l.spec.Pagination.NextField, nextVal)
	parsedURL.RawQuery = q.Encode()

	return parsedURL.String(), nil
}
