package middleware

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

const contextUserIDKey = "userID"
const contextClaimsKey = "claims"

func RequireJWTAuth(jwtSecret []byte, issuer string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := c.Request().Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")

			token, err := jwt.Parse(tokenStr,
				func(t *jwt.Token) (any, error) {
					return jwtSecret, nil
				},
				jwt.WithValidMethods([]string{"HS256"}),
				jwt.WithAudience("authenticated"),
				jwt.WithIssuer(issuer),
				jwt.WithExpirationRequired())
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired JWT").SetInternal(err)
			}
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid token claims").SetInternal(errors.New("jwt claims were not MapClaims"))
			}
			sub, ok := claims["sub"].(string)
			if !ok || sub == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid userID").SetInternal(errors.New("claims subject is invalid"))
			}
			c.Set(contextUserIDKey, sub)
			c.Set(contextClaimsKey, claims)
			return next(c)
		}
	}
}

func RequireAdmin() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			claims, ok := c.Get(contextClaimsKey).(jwt.MapClaims)
			if !ok {
				return echo.NewHTTPError(http.StatusInternalServerError, "server error").SetInternal(errors.New("server configuration error, jwt claims was never set by previous middleware"))
			}

			meta, _ := claims["app_metadata"].(map[string]any)
			role, _ := meta["role"].(string)
			if role != "admin" {
				return echo.NewHTTPError(http.StatusForbidden, "not authorized").SetInternal(errors.New("non admin user is not authorized"))
			}
			return next(c)
		}
	}
}

func RequireAPIKeyAuth(adminAPIKey []byte) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := c.Request().Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")

			if subtle.ConstantTimeCompare([]byte(tokenStr), adminAPIKey) == 0 {
				return echo.NewHTTPError(http.StatusUnauthorized, "access not granted").SetInternal(errors.New("invalid API key"))
			}
			slog.Info("request authenticated with API key", slog.String("requestID", GetRequestID(c)))
			c.Set(contextUserIDKey, "admin")
			return next(c)
		}
	}
}

// UserIDFromContext returns the authenticated user's Supabase ID.
// Only valid inside handlers protected by RequireAuth.
func UserIDFromContext(c echo.Context) string {
	userID, _ := c.Get(contextUserIDKey).(string)
	return userID
}
