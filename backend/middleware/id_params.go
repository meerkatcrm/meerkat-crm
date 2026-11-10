package middleware

import (
	apperrors "meerkat/errors"
	"strconv"

	"github.com/gin-gonic/gin"
)

// NumericIDParamsMiddleware rejects a request whose path parameters are not
// positive integers. Every path parameter under /api/v1 is a row id.
//
// This is a guard, not the only defence: GORM treats a non-numeric string passed
// as an inline condition (db.First(&row, id)) as raw SQL, so a handler that
// forgets to bind its id ("id = ?") would otherwise be injectable.
func NumericIDParamsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, param := range c.Params {
			if id, err := strconv.ParseUint(param.Value, 10, 64); err != nil || id == 0 {
				apperrors.AbortWithError(c, apperrors.ErrInvalidInput(param.Key, "must be a positive integer"))
				return
			}
		}
		c.Next()
	}
}
