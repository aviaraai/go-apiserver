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

func RequireJWTAuth(jwtSecret, adminAPIKey []byte) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := c.Request().Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")

			// Special API key path: constant-time compare to avoid timing attacks.
			if subtle.ConstantTimeCompare([]byte(tokenStr), adminAPIKey) == 1 {
				slog.Info("request authenticated with API key", slog.String("requestID", GetRequestID(c)))
				c.Set(contextUserIDKey, "admin")
				return next(c)
			}

			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing method")
				}
				return jwtSecret, nil
			})
			if err != nil || !token.Valid {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired JWT").SetInternal(err)
			}
			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid token claims").SetInternal(err)
			}
			sub, ok := claims["sub"].(string)
			if !ok || sub == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid subject")
			}
			c.Set(contextUserIDKey, sub)
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
