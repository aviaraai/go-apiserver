package middleware

import (
	"github.com/labstack/echo/v4"
)

const contextClientInfoKey = "clientInfo"

type DeviceInfo struct {
	Manufacturer string
	Model        string
	OSVersion    string
	AppVersion   string
}

func DeviceInfoMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			manufacturer := c.Request().Header.Get("X-Device-Manufacturer")
			model := c.Request().Header.Get("X-Device-Model")
			osVersion := c.Request().Header.Get("X-OS-Version")
			appVersion := c.Request().Header.Get("X-App-Version")

			c.Set(contextClientInfoKey, DeviceInfo{
				Manufacturer: manufacturer,
				Model:        model,
				OSVersion:    osVersion,
				AppVersion:   appVersion,
			})

			return next(c)
		}
	}
}

func DeviceInfoFromContext(c echo.Context) (DeviceInfo, bool) {
	device, ok := c.Get(contextClientInfoKey).(DeviceInfo)
	return device, ok
}
