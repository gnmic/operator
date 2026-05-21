package http

import (
	"fmt"
	"net/http"
)

func (l *Loader) applyAuthorization(req *http.Request) {
	auth := l.spec.Authorization
	if auth == nil {
		return
	}

	switch {
	case auth.Basic != nil:
		req.SetBasicAuth(
			auth.Basic.Username,
			auth.Basic.Password,
		)

	case auth.Token != nil:
		req.Header.Set(
			"Authorization",
			fmt.Sprintf("%s %s",
				auth.Token.Scheme,
				auth.Token.Token,
			),
		)

		// case auth.JWT != nil:
		// 	if auth.JWT.Token != "" {
		// 		req.Header.Set(
		// 			"Authorization",
		// 			fmt.Sprintf("Bearer %s", auth.JWT.Token),
		// 		)
		// 	}
	}
}
