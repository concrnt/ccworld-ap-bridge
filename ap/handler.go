package ap

import (
	"net/http"
	"slices"
	"strings"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"

	"github.com/concrnt/ccworld-ap-bridge/types"
)

var tracer = otel.Tracer("activitypub")

type Handler struct {
	service *Service
}

func NewHandler(service *Service) Handler {
	return Handler{service}
}

func (h Handler) WebFinger(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "WebFinger")
	defer span.End()

	resource := c.QueryParam("resource")
	result, err := h.service.WebFinger(ctx, resource)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusInternalServerError, "Internal server error: "+err.Error())
	}

	c.Response().Header().Set("Content-Type", "application/jrd+json")
	return c.JSON(http.StatusOK, result)
}

// NodeInfo handles nodeinfo requests
func (h Handler) NodeInfo(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "NodeInfo")
	defer span.End()

	result, err := h.service.NodeInfo(ctx)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusInternalServerError, "Internal server error: "+err.Error())
	}

	c.Response().Header().Set("Content-Type", "application/json")
	return c.JSON(http.StatusOK, result)
}

func (h Handler) NodeInfoWellKnown(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "NodeInfoWellKnown")
	defer span.End()

	result, err := h.service.NodeInfoWellKnown(ctx)

	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusInternalServerError, "Internal server error: "+err.Error())
	}

	c.Response().Header().Set("Content-Type", "application/json")
	return c.JSON(http.StatusOK, result)
}

// --

func (h Handler) User(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "User")
	defer span.End()

	id := c.Param("id")
	if id == "" {
		return c.String(http.StatusBadRequest, "Invalid username")
	}

	acceptHeader := c.Request().Header.Get("Accept")
	accept := strings.Split(acceptHeader, ",")

	// check if accept is application/activity+json or application/ld+json
	if !slices.Contains(accept, "application/activity+json") && !slices.Contains(accept, "application/ld+json") {
		// redirect to user page
		redirectURL, err := h.service.GetUserWebURL(ctx, id)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusNotFound, "entity not found")
		}
		return c.Redirect(http.StatusFound, redirectURL)
	}

	result, err := h.service.User(ctx, id)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	c.Response().Header().Set("Content-Type", "application/activity+json")
	return c.JSON(http.StatusOK, result)

}

func (h Handler) Note(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Note")
	defer span.End()

	id := c.Param("id")
	if id == "" {
		return c.String(http.StatusBadRequest, "Invalid noteID")
	}

	// check if accept is application/activity+json or application/ld+json
	acceptHeader := c.Request().Header.Get("Accept")
	accept := strings.Split(acceptHeader, ",")

	if !slices.Contains(accept, "application/activity+json") && !slices.Contains(accept, "application/ld+json") {
		// redirect to user page
		redirectURL, err := h.service.GetNoteWebURL(ctx, id)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusNotFound, "note not found")
		}

		return c.Redirect(http.StatusFound, redirectURL)
	}

	result, err := h.service.Note(ctx, id)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "note not found")
	}

	c.Response().Header().Set("Content-Type", "application/activity+json")
	return c.JSON(http.StatusOK, result)
}

func (h Handler) Inbox(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "HandlerAPInbox")
	defer span.End()

	var object types.ApObject
	err := c.Bind(&object)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusBadRequest, "Invalid request body")
	}

	result, err := h.service.Inbox(ctx, object)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusInternalServerError, "Internal server error: "+err.Error())
	}

	c.Response().Header().Set("Content-Type", "application/activity+json")
	return c.JSON(http.StatusOK, result)
}
