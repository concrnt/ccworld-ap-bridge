package api

import (
	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"net/http"

	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/totegamma/concurrent/core"
)

var tracer = otel.Tracer("api")

type Handler struct {
	service *Service
}

func NewHandler(service *Service) Handler {
	return Handler{
		service,
	}
}

func (h Handler) GetPerson(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "GetPerson")
	defer span.End()

	id := c.Param("id")
	if id == "" {
		return c.String(http.StatusBadRequest, "Invalid username")
	}

	person, err := h.service.GetPerson(ctx, id)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	c.Response().Header().Set("Content-Type", "application/activity+json")
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": person})
}

// UpdatePerson handles entity updates.
func (h Handler) UpdatePerson(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "UpdatePerson")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	var person types.ApPerson
	err := c.Bind(&person)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusBadRequest, "Invalid request body")
	}

	created, err := h.service.UpdatePerson(ctx, requester, person)

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": created})
}

/*
// Follow handles entity follow requests.
func (h Handler) Follow(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Follow")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	targetID := c.Param("id")
	if targetID == "" {
		return c.String(http.StatusBadRequest, "Invalid username")
	}

	if targetID[0] != '@' {
		targetID = "@" + targetID
	}

	log.Println("follow", targetID)

    follow, err := h.service.Follow(ctx, requester, targetID)
    if err != nil {
        span.RecordError(err)
        return c.String(http.StatusNotFound, "entity not found")
    }

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": follow})
}

// Unfollow handles entity unfollow requests.
func (h Handler) UnFollow(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Unfollow")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	targetID := c.Param("id")
	if targetID == "" {
		return c.String(http.StatusBadRequest, "Invalid username")
	}

	if targetID[0] != '@' {
		targetID = "@" + targetID
	}

    deleted, err := h.service.UnFollow(ctx, requester, targetID)
    if err != nil {
        span.RecordError(err)
        return c.String(http.StatusNotFound, "entity not found")
    }

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": deleted})
}
*/

// CreateEntityRequest is a struct for a request to create an entity.
type CreateEntityRequest struct {
	ID string `json:"id"`
}

// CreateEntity handles entity creation.
func (h Handler) CreateEntity(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "CreateEntity")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	var request CreateEntityRequest
	err := c.Bind(&request)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusBadRequest, "Invalid request body")
	}

	entity, err := h.service.CreateEntity(ctx, requester, request.ID)

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entity})
}

// GetEntityID handles entity id requests.
func (h Handler) GetEntityID(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "GetEntityID")
	defer span.End()

	ccid := c.Param("ccid")
	if ccid == "" {
		return c.String(http.StatusBadRequest, "Invalid username")
	}

	entity, err := h.service.GetEntityID(ctx, ccid)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entity})
}
