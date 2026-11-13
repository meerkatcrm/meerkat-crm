package controllers

import (
	"context"
	"meerkat/api"
	"meerkat/clock"
	"meerkat/config"
	apperrors "meerkat/errors"
	"meerkat/middleware"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// request is what an operation handler (see package api) takes from the Gin
// request: the values the middleware set.
type request struct {
	c      *gin.Context
	db     *gorm.DB
	userID uint
}

// requestOf returns the request an operation is serving. It fails with 401 when no
// user is authenticated, like currentUserID.
func requestOf(ctx context.Context) (request, error) {
	c := api.Gin(ctx)
	if c == nil {
		return request{}, apperrors.ErrInternal("operation called without a request")
	}
	userID, ok := c.Value("userID").(uint)
	if !ok {
		return request{}, apperrors.ErrUnauthorized("Authentication required")
	}
	db, ok := c.Value("db").(*gorm.DB)
	if !ok {
		return request{}, apperrors.ErrInternal("no database on the request")
	}
	return request{c: c, db: db, userID: userID}, nil
}

func (r request) cfg() config.Config { return currentConfig(r.c) }

// clock is the request clock, for work that outlives the request (webhook deliveries).
func (r request) clock() clock.Clock { return middleware.RequestClock(r.c) }

// out is the output of an operation whose response is just a JSON body.
type out[T any] struct{ Body T }

func respond[T any](body T) *out[T] { return &out[T]{Body: body} }

// MessageBody is the body of responses that only confirm what happened.
type MessageBody struct {
	Message string `json:"message"`
}
