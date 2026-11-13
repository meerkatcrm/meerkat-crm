// Package api serves huma operations on the existing Gin groups. The Go input and
// output types of an operation are its contract: cmd/openapi writes them to
// docs/openapi.json, and the frontend's types are generated from that file.
//
// Gin middleware (auth, rate limits, request ID, the numeric id guard) still runs
// around every operation, and errors keep the AppError JSON format.
//
// Writing an operation:
//
//   - Register it with Register, not huma.Register.
//   - The handler is func(ctx context.Context, in *Input) (*Output, error). Gin(ctx)
//     returns the request's Gin context, for the db, user and config the middleware set.
//   - Return *apperrors.AppError for failures, as Gin handlers do.
//   - A request body type validates itself: give it a Resolve method that calls
//     ValidateBody, so the go-playground `validate` tags (and the custom validators)
//     apply as they did with ValidateJSONMiddleware.
//   - A body field is required unless its json tag has omitempty. Mark optional
//     fields omitempty; required ones also need validate:"required", which gives
//     the error message.
//   - A request body ignores unknown properties, as ShouldBindJSON did, only with
//     the marker field AcceptUnknownFields. Responses stay closed, so the generated
//     TypeScript types have no index signature.
//   - Mark slice fields of responses nullable:"false" (and never return nil).
//   - Use separate response types rather than GORM models: a model drags its
//     associations and gorm.DeletedAt into the contract.
package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	apperrors "meerkat/errors"
	"meerkat/middleware"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
)

func init() {
	// Errors huma raises itself (validation, malformed JSON) use the AppError format.
	huma.NewError = newError
}

// NewSpec returns the OpenAPI document that every API mounted with it adds its
// operations to, so all groups share one spec.
func NewSpec() *huma.OpenAPI {
	return &huma.OpenAPI{
		OpenAPI: "3.1.0",
		Info: &huma.Info{
			Title:       "Meerkat CRM API",
			Version:     "1",
			Description: `Errors use the AppError format: {"error": {"code", "message", "details"}, "request_id", "timestamp"}.`,
		},
		Servers: []*huma.Server{{URL: "/api/v1"}},
	}
}

// Mount returns a huma API whose operations are served on group, so the group's
// middleware runs around them. It serves no docs or spec routes.
func Mount(engine *gin.Engine, group *gin.RouterGroup, spec *huma.OpenAPI) huma.API {
	cfg := huma.Config{
		OpenAPI:       spec,
		Formats:       huma.DefaultFormats,
		DefaultFormat: "application/json",
		Transformers:  []huma.Transformer{errorTransformer},
	}
	a := humagin.NewWithGroup(engine, group, cfg)
	a.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithValue(ctx, ginContextKey{}, humagin.Unwrap(ctx)))
	})
	return a
}

// Register is huma.Register with the body size limit the global body-limit
// middleware enforces (huma's own default is 1 MiB). Use it for every operation.
func Register[I, O any](a huma.API, op huma.Operation, handler func(context.Context, *I) (*O, error)) {
	if op.MaxBodyBytes == 0 {
		op.MaxBodyBytes = middleware.DefaultMaxBodySize
	}
	huma.Register(a, op, handler)
}

// AcceptUnknownFields, embedded in a request body type, makes the body ignore
// properties it does not declare instead of rejecting them.
type AcceptUnknownFields struct {
	_ struct{} `json:"-" additionalProperties:"true"`
}

type ginContextKey struct{}

// Gin returns the Gin context of the request an operation is serving.
func Gin(ctx context.Context) *gin.Context {
	c, _ := ctx.Value(ginContextKey{}).(*gin.Context)
	return c
}

// ValidateBody runs the `validate` tags of body, keyed by JSON field name like
// huma's own errors. Call it from the body type's Resolve
// method:
//
//	func (b *NoteBody) Resolve(_ huma.Context, prefix *huma.PathBuffer) []error {
//		return api.ValidateBody(prefix, b)
//	}
func ValidateBody(prefix *huma.PathBuffer, body any) []error {
	var errs []error
	for _, ve := range middleware.ValidateStructJSON(body) {
		errs = append(errs, &huma.ErrorDetail{Message: ve.Message, Location: prefix.With(ve.Field)})
	}
	return errs
}

// Error is the body of every error response: the AppError format. It is what
// huma documents as the error schema and writes as the body.
type Error struct {
	apperrors.ErrorResponse
	app *apperrors.AppError
}

// GetStatus makes Error a huma.StatusError.
func (e *Error) GetStatus() int { return e.app.HTTPStatus }

func (e *Error) Error() string { return e.app.Error() }

func wrap(e *apperrors.AppError) *Error {
	return &Error{
		ErrorResponse: apperrors.ErrorResponse{Error: apperrors.ErrorDetail{Code: e.Code, Message: e.Message, Details: e.Details}},
		app:           e,
	}
}

// newError replaces huma.NewError. huma calls it for the errors it raises itself
// (validation, malformed JSON) and for any error a handler returns, so an
// *apperrors.AppError from a handler keeps its status and code, and huma's own
// errors get the status codes the API used before (400 for validation, not 422).
func newError(status int, msg string, errs ...error) huma.StatusError {
	if len(errs) > 0 {
		var app *apperrors.AppError
		if errors.As(errs[0], &app) {
			return wrap(app)
		}
	}
	switch status {
	case http.StatusUnprocessableEntity:
		e := apperrors.ErrValidation("Request validation failed")
		var required []string // fields huma reports as missing
		for _, err := range errs {
			d, ok := err.(huma.ErrorDetailer)
			if !ok {
				e.WithDetails("request", err.Error())
				continue
			}
			detail := d.ErrorDetail()
			if rest, missing := strings.CutPrefix(detail.Message, "expected required property "); missing {
				name, _, _ := strings.Cut(rest, " ") // "content to be present"
				// Reported on the parent object, with the property name in the message.
				required = append(required, fieldOf(detail.Location+"."+name))
				continue
			}
			e.WithDetails(fieldOf(detail.Location), detail.Message)
		}
		// A missing field also fails its validate:"required" tag, which says so in the
		// project's wording; keep huma's report only for a field without that tag.
		for _, field := range required {
			if _, reported := e.Details[field]; !reported {
				e.WithDetails(field, field+" is required")
			}
		}
		return wrap(e)
	case http.StatusBadRequest:
		// Malformed JSON: the same error ValidateJSONMiddleware gave, with the parser's reason.
		reason := msg
		if len(errs) > 0 {
			reason = errs[0].Error()
			if d, ok := errs[0].(huma.ErrorDetailer); ok {
				reason = d.ErrorDetail().Message
			}
		}
		return wrap(apperrors.ErrInvalidInput("request body", reason))
	case http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType:
		return wrap(apperrors.NewError(apperrors.ErrCodeInvalidInput, msg, status))
	}
	e := apperrors.NewError(codeFor(status), msg, status)
	if len(errs) > 0 {
		e = e.WithError(errs[0]) // logged, never sent
	}
	return wrap(e)
}

func codeFor(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return apperrors.ErrCodeUnauthorized
	case http.StatusForbidden:
		return apperrors.ErrCodeForbidden
	case http.StatusNotFound:
		return apperrors.ErrCodeNotFound
	case http.StatusConflict:
		return apperrors.ErrCodeConflict
	case http.StatusTooManyRequests:
		return apperrors.ErrCodeRateLimitExceeded
	}
	if status >= 500 {
		return apperrors.ErrCodeInternal
	}
	return apperrors.ErrCodeOperationFailed
}

// fieldOf turns a huma location such as "body.content" into the field name.
func fieldOf(location string) string {
	for _, prefix := range []string{"body.", "query.", "path.", "header."} {
		if rest, ok := strings.CutPrefix(location, prefix); ok {
			return rest
		}
	}
	if location == "" || location == "body" {
		return "request body"
	}
	return location
}

// errorTransformer logs an error response and stamps it with the request ID and
// time, as apperrors.RespondWithError does.
func errorTransformer(ctx huma.Context, _ string, v any) (any, error) {
	e, ok := v.(*Error)
	if !ok {
		return v, nil
	}
	c := humagin.Unwrap(ctx)
	apperrors.LogError(c, e.app)
	e.RequestID = c.GetString("request_id")
	e.Timestamp = time.Now().UTC().Format(time.RFC3339)
	return e, nil
}
