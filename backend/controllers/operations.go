package controllers

import (
	"context"
	"meerkat/api"
	"meerkat/config"
	apperrors "meerkat/errors"

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
	db, _ := c.Value("db").(*gorm.DB)
	return request{c: c, db: db, userID: userID}, nil
}

func (r request) cfg() config.Config { return currentConfig(r.c) }

// MessageBody is the body of responses that only confirm what happened.
type MessageBody struct {
	Message string `json:"message"`
}

type messageOutput struct{ Body MessageBody }

func message(text string) *messageOutput {
	return &messageOutput{Body: MessageBody{Message: text}}
}
