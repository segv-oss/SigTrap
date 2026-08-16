package middleware

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"SigTrap-backend/internal/config"

	"golang.org/x/time/rate"
)

// ErrorResponse defines the standardized error JSON output structure.
type ErrorDetail struct {
	Field string `json:"field,omitempty"`
	Issue string `json:"issue"`
}

type ErrorBody struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Details []ErrorDetail `json:"details,omitempty"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// WriteJSONError sends a structured JSON error response.
func WriteJSONError(w http.ResponseWriter, statusCode int, code, message string, details ...ErrorDetail) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorBody{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}

// CORS middleware adds cross-origin resource sharing headers and handles OPTIONS preflight.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-SigTrap-Project-Key")
		w.Header().Set("Access-Control-Expose-Headers", "X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ProjectKeyLimiter manages rate limiters per project key.
type ProjectKeyLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	rps      rate.Limit
	burst    int
}

// NewProjectKeyLimiter initializes a rate limiter manager.
func NewProjectKeyLimiter(rps float64, burst int) *ProjectKeyLimiter {
	return &ProjectKeyLimiter{
		limiters: make(map[string]*rate.Limiter),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
}

func (pkl *ProjectKeyLimiter) getLimiter(projectKey string) *rate.Limiter {
	pkl.mu.RLock()
	limiter, exists := pkl.limiters[projectKey]
	pkl.mu.RUnlock()

	if exists {
		return limiter
	}

	pkl.mu.Lock()
	defer pkl.mu.Unlock()

	// Double check after lock
	if limiter, exists = pkl.limiters[projectKey]; exists {
		return limiter
	}

	limiter = rate.NewLimiter(pkl.rps, pkl.burst)
	pkl.limiters[projectKey] = limiter
	return limiter
}

// RateLimit middleware enforces rate limits per project key.
func (pkl *ProjectKeyLimiter) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projectKey := r.Header.Get("X-SigTrap-Project-Key")
		if projectKey == "" {
			projectKey = r.URL.Query().Get("project_id")
		}
		if projectKey == "" {
			projectKey = "anonymous"
		}

		limiter := pkl.getLimiter(projectKey)
		if !limiter.Allow() {
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.0f", pkl.rps))
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Second).Unix()))

			WriteJSONError(w, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED",
				"Rate limit exceeded for this project key. Please retry later.")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireProjectKey middleware ensures X-SigTrap-Project-Key header is present on requests.
func RequireProjectKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projectKey := r.Header.Get("X-SigTrap-Project-Key")
		if projectKey == "" {
			WriteJSONError(w, http.StatusUnauthorized, "MISSING_PROJECT_KEY",
				"Header 'X-SigTrap-Project-Key' is required for this route.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdminToken middleware verifies Bearer token against cfg.AdminToken.
func RequireAdminToken(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				WriteJSONError(w, http.StatusUnauthorized, "UNAUTHORIZED",
					"Authorization header is required.")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				WriteJSONError(w, http.StatusUnauthorized, "INVALID_AUTH_FORMAT",
					"Authorization header format must be 'Bearer <token>'.")
				return
			}

			if parts[1] != cfg.AdminToken {
				WriteJSONError(w, http.StatusForbidden, "FORBIDDEN",
					"Invalid admin access token.")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Logger middleware logs incoming HTTP request details.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Printf("[HTTP] %s %s %d - %v", r.Method, r.URL.Path, rw.statusCode, time.Since(start))
	})
}

// Recovery middleware handles panics safely.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[PANIC RECOVERY] %v", err)
				WriteJSONError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR",
					"An unexpected internal error occurred.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
