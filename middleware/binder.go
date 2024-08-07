package middleware

import (
	"github.com/labstack/echo/v4"
	"strings"
)

// ApBinder is a binder for the ActivityPub protocol.
type Binder struct{}

// NewApBinder returns a new ApBinder.
func (cb *Binder) Bind(i interface{}, c echo.Context) (err error) {
	db := new(echo.DefaultBinder)

	contentTypeFull := c.Request().Header.Get(echo.HeaderContentType)
	split := strings.Split(contentTypeFull, ";")
	contentType := split[0]

	if contentType == "application/activity+json" || contentType == "application/ld+json" {
		c.Request().Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	return db.Bind(i, c)
}
