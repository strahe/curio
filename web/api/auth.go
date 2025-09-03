package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/filecoin-project/curio/web/api/apihelper"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/gorilla/mux"
)

func authMiddleware(
	authVerify func(context.Context, string) ([]auth.Permission, error),
	enabled bool) mux.MiddlewareFunc {

	return func(next http.Handler) http.Handler {
		if !enabled {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				writeUnauthorized(w, "missing authentication token")
				return
			}

			_, err := authVerify(r.Context(), token)
			if err != nil {
				apihelper.OrHTTPFail(w, errors.New("invalid token"))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractToken from HTTP request
// Check priority: Authorization header -> Cookie -> URL parameter
func extractToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		// Check Bearer format: Authorization: Bearer <token>
		const bearerPrefix = "Bearer "
		if after, ok := strings.CutPrefix(authHeader, bearerPrefix); ok {
			token := strings.TrimSpace(after)
			if token != "" {
				return token
			}
		}
	}

	if cookie, err := r.Cookie("curio_token"); err == nil && cookie.Value != "" {
		return strings.TrimSpace(cookie.Value)
	}

	if token := r.URL.Query().Get("token"); token != "" {
		return strings.TrimSpace(token)
	}

	return ""
}

func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
