package servermanager

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
)

const bearerPrefix = "Bearer "

// BearerTokenMiddleware returns middleware that requires an Authorization header
// of the form "Bearer <token>" matching one of the configured tokens. If no
// tokens are configured, every request is rejected with 401 — i.e. the API is
// effectively disabled until at least one token is set in config.yml.
func BearerTokenMiddleware(tokens []string) func(http.Handler) http.Handler {
	valid := make([][]byte, 0, len(tokens))
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t != "" {
			valid = append(valid, []byte(t))
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(valid) == 0 {
				http.Error(w, "API disabled: no api_tokens configured", http.StatusUnauthorized)
				return
			}

			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, bearerPrefix) {
				http.Error(w, "missing or malformed Authorization header", http.StatusUnauthorized)
				return
			}

			provided := []byte(strings.TrimSpace(header[len(bearerPrefix):]))
			if len(provided) == 0 {
				http.Error(w, "empty bearer token", http.StatusUnauthorized)
				return
			}

			for _, v := range valid {
				if subtle.ConstantTimeCompare(provided, v) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}

			logrus.Warnf("api: rejected request from %s with invalid bearer token", r.RemoteAddr)
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
		})
	}
}
