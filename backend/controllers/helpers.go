package controllers

import (
	"database/sql"
	"errors"
	"meerkat/config"
	apperrors "meerkat/errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	defaultPage  = 1
	defaultLimit = 25
	maxLimit     = 100
)

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// likeAny returns a condition that is true when any of exprs contains the search
// term bound by searchArg. The ESCAPE clause lives here so no caller can forget it;
// a query that uses likeAny without searchArg fails instead of matching wrongly.
func likeAny(exprs ...string) string {
	conds := make([]string, len(exprs))
	for i, expr := range exprs {
		conds[i] = expr + ` LIKE @search ESCAPE '\'`
	}
	return strings.Join(conds, " OR ")
}

// searchArg binds a user search term for likeAny, escaping the LIKE wildcards so
// that "50%" or "a_b" match literally. Like ftsMatch it uses the first maxSearchRunes.
func searchArg(term string) sql.NamedArg {
	if r := []rune(term); len(r) > maxSearchRunes {
		term = string(r[:maxSearchRunes])
	}
	return sql.Named("search", "%"+likeEscaper.Replace(term)+"%")
}

// Bounds on what one search can ask the full-text index for.
const (
	maxSearchRunes = 200
	maxSearchWords = 32
)

// phoneLike is input of only digits and phone punctuation: "795550" must find the middle of
// "+41 79 5550101", which a word-prefix index cannot do.
var phoneLike = regexp.MustCompile(`^[\d\s+()./-]*\d[\d\s+()./-]*$`)

// unspacedScripts are written without spaces between words, so the tokenizer indexes a
// whole phrase as one word and a prefix search cannot find a word inside it.
var unspacedScripts = []*unicode.RangeTable{
	unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul,
	unicode.Thai, unicode.Lao, unicode.Khmer, unicode.Myanmar, unicode.Tibetan,
}

// ftsMatch turns a typed search term into an FTS5 MATCH expression for the *_fts tables of
// migration 41: every word must match the start of an indexed word, in any order ("mül zo"
// finds "Zoë Müller"; case and accents are ignored by the tokenizer). Each word is quoted, so
// FTS5 syntax in the input is plain text. ok is false when the caller should use the LIKE
// search instead: no letters or digits, phone-like digits, or text in a script written
// without spaces between words (unspacedScripts), where a typed part must match anywhere.
func ftsMatch(term string) (match string, ok bool) {
	if r := []rune(term); len(r) > maxSearchRunes {
		term = string(r[:maxSearchRunes])
	}
	if phoneLike.MatchString(term) {
		return "", false
	}
	for _, r := range term {
		if unicode.In(r, unspacedScripts...) {
			return "", false
		}
	}
	words := strings.FieldsFunc(term, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) == 0 {
		return "", false
	}
	if len(words) > maxSearchWords {
		words = words[:maxSearchWords]
	}
	for i, w := range words {
		words[i] = `"` + w + `"*`
	}
	return strings.Join(words, " "), true
}

// PaginationParams represents sanitized pagination query values.
type PaginationParams struct {
	Page   int
	Limit  int
	Offset int
}

func currentUserID(c *gin.Context) (uint, bool) {
	value, exists := c.Get("userID")
	if !exists {
		apperrors.AbortWithError(c, apperrors.ErrUnauthorized("Authentication required"))
		return 0, false
	}

	userID, ok := value.(uint)
	if !ok {
		apperrors.AbortWithError(c, apperrors.ErrUnauthorized("Authentication required"))
		return 0, false
	}

	return userID, true
}

func currentConfig(c *gin.Context) config.Config {
	if val, exists := c.Get("cfg"); exists {
		if cfg, ok := val.(config.Config); ok {
			return cfg
		}
	}
	return config.Config{}
}

// GetPaginationParams extracts pagination query params using shared defaults and bounds.
func GetPaginationParams(c *gin.Context) PaginationParams {
	return paginationOf(c.Query("page"), c.Query("limit"))
}

// paginationOf sanitizes raw page and limit values: a missing or unparsable one
// falls back to its default.
func paginationOf(rawPage, rawLimit string) PaginationParams {
	page := parsePositiveOrDefault(rawPage, defaultPage)
	limit := parsePositiveOrDefault(rawLimit, defaultLimit)
	if limit > maxLimit {
		limit = maxLimit
	}

	return PaginationParams{
		Page:   page,
		Limit:  limit,
		Offset: (page - 1) * limit,
	}
}

func parsePositiveOrDefault(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

// findOwned loads the row of type T with the given id that belongs to userID, or
// answers 404 (500 on a database error) and returns false. Pass a db that already
// carries any Preload. The id is always bound as a value: GORM runs a non-numeric
// string given as an inline condition as raw SQL.
func findOwned[T any](c *gin.Context, db *gorm.DB, userID uint, id any, resource string) (T, bool) {
	row, err := loadOwned[T](db, userID, id, resource)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return row, false
	}
	return row, true
}

// loadOwned is findOwned for operation handlers, which return the error instead
// of writing it.
func loadOwned[T any](db *gorm.DB, userID uint, id any, resource string) (T, *apperrors.AppError) {
	var row T
	if err := db.Where("user_id = ?", userID).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return row, apperrors.ErrNotFound(resource).WithDetails("id", id)
		}
		return row, apperrors.ErrDatabase("Failed to retrieve " + strings.ToLower(resource)).WithError(err)
	}
	return row, nil
}
