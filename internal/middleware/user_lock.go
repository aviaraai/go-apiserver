package middleware

import (
	"sync"

	"github.com/labstack/echo/v4"
)

var userLock sync.Map

func UserLockMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID := UserIDFromContext(c)
		lock := GetUserLock(userID)
		lock.Lock()
		defer lock.Unlock()
		return next(c)
	}
}

func GetUserLock(userID string) *sync.Mutex {
	m, _ := userLock.LoadOrStore(userID, &sync.Mutex{})
	return m.(*sync.Mutex)
}
