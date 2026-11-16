package errors

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// A cache is allowed to invent a freshness lifetime for a 404 unless the response rules it
// out (RFC 9111 4.2.2), which would keep a contact's picture missing after the file came
// back. Every error answer goes through RespondWithError, so the ban belongs there.
func TestErrorResponsesAreNotCacheable(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	for _, appErr := range []*AppError{ErrNotFound("Thumbnail"), ErrValidation("nope"), ErrInternal("boom")} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		// FromContext reads the request out of the context to annotate the log line.
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/contacts/1/profile_picture", nil)
		RespondWithError(c, appErr)
		assert.Equal(t, appErr.HTTPStatus, w.Code, appErr.Message)
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), appErr.Message)
	}
}
