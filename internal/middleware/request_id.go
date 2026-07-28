package middleware

import "github.com/labstack/echo/v4"

func GetRequestID(c echo.Context) string {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	return requestID
}
