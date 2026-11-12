package services

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"meerkat/config"

	"meerkat/models"
	"meerkat/monica"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A gravatar URL comes from the Monica server, so it is a user-supplied image URL: it goes
// through the image URL guard even when the Monica instance itself may be private
// (docs/deployment.md, "Outgoing requests").
func TestMonicaGravatarFromAnInternalAddressIsNeverFetched(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
	}))
	defer srv.Close()

	db := setupTestDB(t)
	client, err := monica.NewClient(srv.URL, "tok", false) // a private Monica instance is allowed
	require.NoError(t, err)
	contact := models.Contact{UserID: 1, Firstname: "Eve"}
	require.NoError(t, db.Create(&contact).Error)

	s := &monicaImportSession{client: client, result: &models.MonicaImportResult{}}
	log := zerolog.Nop()
	(&MonicaImportManager{}).processAvatars(db, s, []avatarTask{{
		contactID: contact.ID, contactName: "Eve", avatarURL: srv.URL + "/internal/avatar.png", source: avatarSourceGravatar,
	}}, &config.Config{ProfilePhotoDir: t.TempDir()}, &log)

	assert.Equal(t, 1, s.result.PhotosFailed)
	assert.Zero(t, hits.Load(), "an internal avatar URL must not be requested")
}
