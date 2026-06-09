package http

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// extractNextPageInfo extracts pagination information from a response
func (l *Loader) extractNextPageInfo(raw any) (string, error) {
	if l.spec.Pagination == nil || l.spec.Pagination.NextField == "" {
		return "", nil
	}

	// Extract next value
	prog, err := compileCEL(l.spec.Pagination.NextField)
	if err != nil {
		return "", fmt.Errorf("invalid NextField CEL: %w", err)
	}
	out, _, err := prog.Eval(map[string]any{"self": raw})
	if err != nil {
		return "", fmt.Errorf("CEL eval failed: %w", err)
	}
	if out == nil || out.Value() == nil {
		return "", nil
	}

	str, ok := out.Value().(string)
	if !ok {
		return "", fmt.Errorf("NextField must evaluate to string")
	}

	return str, nil
}

// Link header parsing
func extractNextFromLinkHeader(h http.Header) string {
	link := h.Get("Link")
	if link == "" {
		return ""
	}

	parts := strings.Split(link, ",")
	for _, p := range parts {
		if strings.Contains(p, `rel="next"`) {
			start := strings.Index(p, "<")
			end := strings.Index(p, ">")
			if start != -1 && end != -1 {
				return p[start+1 : end]
			}
		}
	}
	return ""
}

// buildNextURL supports token and full URL
func (l *Loader) buildNextURL(currentURL, nextVal string) (string, error) {
	if parsed, err := url.Parse(nextVal); err == nil && parsed.Scheme != "" {
		return nextVal, nil // full URL
	}

	if l.spec.Pagination.RequestParam == "" {
		return "", fmt.Errorf("requestParam must be set for token pagination")
	}

	parsedURL, err := url.Parse(currentURL)
	if err != nil {
		return "", err
	}

	q := parsedURL.Query()
	q.Set(l.spec.Pagination.RequestParam, nextVal)
	parsedURL.RawQuery = q.Encode()

	return parsedURL.String(), nil
}
