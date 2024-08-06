package api

import (
	"log"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"

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

func (h Handler) GetEntity(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "GetPerson")
	defer span.End()

	ccid := c.Param("ccid")
	if ccid == "" {
		ccid = c.QueryParam("ccid")
	}

	userid := c.QueryParam("id")

	if ccid != "" {
		person, err := h.service.GetEntityByCCID(ctx, ccid)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusNotFound, "entity not found")
		}

		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": person})
	}

	if userid != "" {
		person, err := h.service.GetEntityByID(ctx, userid)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusNotFound, "entity not found")
		}

		return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": person})
	}

	return c.String(http.StatusBadRequest, "Invalid request")
}

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

type UpdateEntityAliasesRequest struct {
	Aliases []string `json:"aliases"`
}

func (h Handler) UpdateEntityAliases(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "UpdateEntityAliases")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	var request UpdateEntityAliasesRequest
	err := c.Bind(&request)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusBadRequest, "Invalid request body")
	}

	entity, err := h.service.UpdateEntityAliases(ctx, requester, request.Aliases)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": entity})
}

func (h Handler) GetStats(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Api.Service.GetStats")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	stats, err := h.service.GetStats(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": stats})
}

func (h Handler) ResolvePerson(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Api.Service.ResolvePerson")
	defer span.End()

	encoded := c.Param("id")
	if encoded == "" {
		return c.String(http.StatusBadRequest, "Invalid username")
	}

	id, err := url.PathUnescape(encoded)
	if err != nil {
		return c.String(http.StatusBadRequest, "Invalid username")
	}

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	person, err := h.service.ResolvePerson(ctx, id, requester)

	c.Response().Header().Set("Content-Type", "application/activity+json")
	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": person})
}

func (h Handler) ImportNote(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Api.Service.ImportNote")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	noteID := c.QueryParams().Get("note")
	if noteID == "" {
		log.Println("invalid noteID: ", noteID)
		return c.String(http.StatusBadRequest, "Invalid noteID")
	}

	message, err := h.service.ImportNote(ctx, noteID, requester)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "note not found")
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": message})
}

func (h Handler) UpdateUserSettings(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Api.Service.UpdateUserSettings")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	var settings types.ApUserSettings
	err := c.Bind(&settings)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusBadRequest, "Invalid request body")
	}

	settings.CCID = requester

	err = h.service.UpsertUserSettings(ctx, settings)
	if err != nil {
		span.RecordError(err)
		log.Println("error upserting user settings: ", err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok"})
}

func (h Handler) GetUserSettings(c echo.Context) error {
	ctx, span := tracer.Start(c.Request().Context(), "Api.Service.GetUserSettings")
	defer span.End()

	requester, ok := ctx.Value(core.RequesterIdCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusForbidden, echo.Map{"status": "error", "message": "requester not found"})
	}

	settings, err := h.service.GetUserSettings(ctx, requester)
	if err != nil {
		span.RecordError(err)
		return c.String(http.StatusNotFound, "entity not found")
	}

	return c.JSON(http.StatusOK, echo.Map{"status": "ok", "content": settings})
}
