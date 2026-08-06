package middleware

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

const contextUserIDKey = "userID"
const contextEmailKey = "email"
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
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid user id").SetInternal(errors.New("claims sub field is invalid"))
			}
			email, ok := claims["email"].(string)
			if !ok || email == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid email").SetInternal(errors.New("claims email field is invalid"))
			}
			c.Set(contextUserIDKey, sub)
			c.Set(contextEmailKey, email)
			c.Set(contextClaimsKey, claims)
			return next(c)
		}
	}
}

func rolesFromClaims(claims jwt.MapClaims) []string {
	meta, _ := claims["app_metadata"].(map[string]any)
	raw, _ := meta["app_roles"].([]any)
	roles := make([]string, 0, len(raw))
	for _, r := range raw {
		if s, ok := r.(string); ok {
			roles = append(roles, s)
		}
	}
	return roles
}

func RequireRole(role string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			claims, ok := c.Get(contextClaimsKey).(jwt.MapClaims)
			if !ok {
				return echo.NewHTTPError(http.StatusInternalServerError, "server error").
					SetInternal(errors.New("jwt claims never set by previous middleware"))
			}
			if !slices.Contains(rolesFromClaims(claims), role) {
				return echo.NewHTTPError(http.StatusForbidden, "not authorized").
					SetInternal(fmt.Errorf("user lacks role %q", role))
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

func UserIDFromContext(c echo.Context) string {
	userID, _ := c.Get(contextUserIDKey).(string)
	return userID
}

func EmailFromContext(c echo.Context) string {
	email, _ := c.Get(contextEmailKey).(string)
	return email
}
