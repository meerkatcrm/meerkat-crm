package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "meerkat/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
)

type thingBody struct {
	AcceptUnknownFields
	Name  string `json:"name" validate:"required,max=5"`
	Count int    `json:"count,omitempty" validate:"omitempty,min=1"`
}

func (b *thingBody) Resolve(_ huma.Context, prefix *huma.PathBuffer) []error {
	return ValidateBody(prefix, b)
}

type thingInput struct {
	ID   uint `path:"id"`
	Body thingBody
}

type thingOutput struct {
	Body struct {
		Name   string `json:"name"`
		UserID uint   `json:"user_id"`
	}
}

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	group := r.Group("/api/v1")
	group.Use(func(c *gin.Context) {
		c.Set("request_id", "req-1")
		c.Set("userID", uint(7))
		c.Next()
	})
	a := Mount(r, group, NewSpec())
	huma.Register(a, huma.Operation{OperationID: "put-thing", Method: http.MethodPut, Path: "/things/{id}"},
		func(ctx context.Context, in *thingInput) (*thingOutput, error) {
			if in.ID == 404 {
				return nil, apperrors.ErrNotFound("Thing")
			}
			out := &thingOutput{}
			out.Body.Name = in.Body.Name
			out.Body.UserID = Gin(ctx).GetUint("userID")
			return out, nil
		})
	return r
}

func put(t *testing.T, r http.Handler, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %q", w.Body.String())
	}
	return w.Code, out
}

func errorOf(t *testing.T, body map[string]any) (code string, details map[string]any) {
	t.Helper()
	e, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("no AppError envelope: %v", body)
	}
	details, _ = e["details"].(map[string]any)
	return e["code"].(string), details
}

func TestOperationSeesTheGinContext(t *testing.T) {
	status, body := put(t, newTestRouter(t), "/api/v1/things/1", `{"name":"ok","extra":true}`)
	if status != http.StatusOK || body["name"] != "ok" || body["user_id"] != float64(7) {
		t.Fatalf("got %d %v", status, body)
	}
}

func TestAppErrorKeepsItsStatusAndFormat(t *testing.T) {
	status, body := put(t, newTestRouter(t), "/api/v1/things/404", `{"name":"ok"}`)
	code, _ := errorOf(t, body)
	if status != http.StatusNotFound || code != apperrors.ErrCodeNotFound {
		t.Fatalf("got %d %s", status, code)
	}
	if body["request_id"] != "req-1" || body["timestamp"] == "" {
		t.Fatalf("not stamped: %v", body)
	}
}

func TestValidationErrorsAre400WithDetailsPerField(t *testing.T) {
	status, body := put(t, newTestRouter(t), "/api/v1/things/1", `{"name":"too long","count":-1}`)
	code, details := errorOf(t, body)
	if status != http.StatusBadRequest || code != apperrors.ErrCodeValidation {
		t.Fatalf("got %d %s", status, code)
	}
	if details["Name"] != "Name must be at most 5 characters" || details["Count"] == nil {
		t.Fatalf("details: %v", details)
	}
}

func TestAMissingFieldIsReportedOnce(t *testing.T) {
	_, body := put(t, newTestRouter(t), "/api/v1/things/1", `{}`)
	_, details := errorOf(t, body)
	if len(details) != 1 || details["Name"] != "Name is required" {
		t.Fatalf("details: %v", details)
	}
}

func TestMalformedJSONIsInvalidInput(t *testing.T) {
	status, body := put(t, newTestRouter(t), "/api/v1/things/1", `{"name":`)
	code, details := errorOf(t, body)
	if status != http.StatusBadRequest || code != apperrors.ErrCodeInvalidInput {
		t.Fatalf("got %d %s", status, code)
	}
	if details["field"] != "request body" || details["reason"] == "" || details["reason"] == "validation failed" {
		t.Fatalf("details: %v", details)
	}
}
