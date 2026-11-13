package controllers

import (
	"context"
	"meerkat/api"
	"meerkat/carddav"
	apperrors "meerkat/errors"
	"meerkat/models"
	"meerkat/services"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RegisterNoteOperations adds the note operations to a, the authenticated API.
func RegisterNoteOperations(a huma.API) {
	tags := []string{"notes"}
	api.Register(a, huma.Operation{
		OperationID: "list-contact-notes", Method: http.MethodGet, Path: "/contacts/{id}/notes",
		Summary: "List the notes about a contact", Tags: tags,
	}, listContactNotes)
	api.Register(a, huma.Operation{
		OperationID: "create-contact-note", Method: http.MethodPost, Path: "/contacts/{id}/notes",
		Summary: "Add a note about a contact", Tags: tags,
	}, createContactNote)
	api.Register(a, huma.Operation{
		OperationID: "list-unassigned-notes", Method: http.MethodGet, Path: "/notes",
		Summary: "List the notes without a contact, with search, date range and paging", Tags: tags,
	}, listUnassignedNotes)
	api.Register(a, huma.Operation{
		OperationID: "create-note", Method: http.MethodPost, Path: "/notes",
		Summary: "Add a note, about a contact or without one", Tags: tags,
	}, createNote)
	api.Register(a, huma.Operation{
		OperationID: "get-note", Method: http.MethodGet, Path: "/notes/{id}",
		Summary: "Get a note", Tags: tags,
	}, getNote)
	api.Register(a, huma.Operation{
		OperationID: "update-note", Method: http.MethodPut, Path: "/notes/{id}",
		Summary: "Replace a note's content, date and contact", Tags: tags,
	}, updateNote)
	api.Register(a, huma.Operation{
		OperationID: "delete-note", Method: http.MethodDelete, Path: "/notes/{id}",
		Summary: "Delete a note", Tags: tags,
	}, deleteNote)
}

// NoteBody is the request body that creates or replaces a note.
type NoteBody struct {
	api.AcceptUnknownFields
	Content   string    `json:"content" validate:"required,min=1,max=5000" maxLength:"5000"`
	Date      time.Time `json:"date" validate:"required"`
	ContactID *uint     `json:"contact_id,omitempty" nullable:"true" minimum:"1" validate:"omitempty,gt=0" doc:"The contact the note is about. Ignored under /contacts/{id}/notes. On update, null or left out detaches the note."`
}

func (b *NoteBody) Resolve(_ huma.Context, prefix *huma.PathBuffer) []error {
	return api.ValidateBody(prefix, b)
}

// Note is a note as the API returns it.
type Note struct {
	ID        uint      `json:"ID"`
	CreatedAt time.Time `json:"CreatedAt"`
	UpdatedAt time.Time `json:"UpdatedAt"`
	Content   string    `json:"content"`
	Date      time.Time `json:"date"`
	ContactID *uint     `json:"contact_id" nullable:"true"`
	Image     string    `json:"image" doc:"File name of the note's image, empty when it has none. Served by GET /notes/{id}/image."`
}

func toNote(n models.Note) Note {
	return Note{
		ID: n.ID, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
		Content: n.Content, Date: n.Date, ContactID: n.ContactID, Image: n.Image,
	}
}

func toNotes(rows []models.Note) []Note {
	notes := make([]Note, len(rows))
	for i, n := range rows {
		notes[i] = toNote(n)
	}
	return notes
}

// NoteSaved is the response to creating or updating a note.
type NoteSaved struct {
	Message string `json:"message"`
	Note    Note   `json:"note"`
}

// ContactNotes is the list of a contact's notes.
type ContactNotes struct {
	Notes []Note `json:"notes" nullable:"false"`
}

// NotePage is one page of notes.
type NotePage struct {
	Notes []Note `json:"notes" nullable:"false"`
	Total int64  `json:"total"`
	Page  int    `json:"page"`
	Limit int    `json:"limit"`
}

type idInput struct {
	ID uint `path:"id" minimum:"1"`
}

type noteBodyInput struct {
	Body NoteBody
}

type idNoteBodyInput struct {
	ID   uint `path:"id" minimum:"1"`
	Body NoteBody
}

func listContactNotes(ctx context.Context, in *idInput) (*out[ContactNotes], error) {
	r, err := requestOf(ctx)
	if err != nil {
		return nil, err
	}
	if _, appErr := loadOwned[models.Contact](r.db, r.userID, in.ID, "Contact"); appErr != nil {
		return nil, appErr
	}
	var notes []models.Note
	if err := r.db.Where("contact_id = ? AND user_id = ?", in.ID, r.userID).Find(&notes).Error; err != nil {
		return nil, apperrors.ErrDatabase("Failed to retrieve notes").WithError(err)
	}
	return respond(ContactNotes{Notes: toNotes(notes)}), nil
}

func createContactNote(ctx context.Context, in *idNoteBodyInput) (*out[NoteSaved], error) {
	r, err := requestOf(ctx)
	if err != nil {
		return nil, err
	}
	contact, appErr := loadOwned[models.Contact](r.db, r.userID, in.ID, "Contact")
	if appErr != nil {
		return nil, appErr
	}
	return saveNewNote(r, in.Body, &contact.ID)
}

func createNote(ctx context.Context, in *noteBodyInput) (*out[NoteSaved], error) {
	r, err := requestOf(ctx)
	if err != nil {
		return nil, err
	}
	if in.Body.ContactID != nil {
		if _, appErr := loadOwned[models.Contact](r.db, r.userID, *in.Body.ContactID, "Contact"); appErr != nil {
			return nil, appErr
		}
	}
	return saveNewNote(r, in.Body, in.Body.ContactID)
}

func saveNewNote(r request, body NoteBody, contactID *uint) (*out[NoteSaved], error) {
	note := models.Note{UserID: r.userID, Content: body.Content, Date: body.Date, ContactID: contactID}
	if err := r.db.Create(&note).Error; err != nil {
		return nil, apperrors.ErrDatabase("Failed to save note").WithError(err)
	}
	go services.TriggerWebhooks(r.db, r.cfg(), r.userID, "note.created", note)
	return respond(NoteSaved{Message: "Note created successfully", Note: toNote(note)}), nil
}

func getNote(ctx context.Context, in *idInput) (*out[Note], error) {
	r, err := requestOf(ctx)
	if err != nil {
		return nil, err
	}
	note, appErr := loadOwned[models.Note](r.db, r.userID, in.ID, "Note")
	if appErr != nil {
		return nil, appErr
	}
	return respond(toNote(note)), nil
}

type listNotesInput struct {
	// Strings, so a value that does not parse falls back to its default instead of failing.
	Page     string `query:"page" doc:"Page number, from 1 (default 1)"`
	Limit    string `query:"limit" doc:"Page size, 1 to 100 (default 25)"`
	Search   string `query:"search" doc:"Words the content contains, each matching the start of a word; case and accents are ignored. Digits-only text, and scripts written without spaces (Chinese, Japanese, Korean, Thai, Lao, Khmer, Myanmar, Tibetan), match anywhere"`
	FromDate string `query:"fromDate" doc:"First day, YYYY-MM-DD; ignored when not a date"`
	ToDate   string `query:"toDate" doc:"Last day, inclusive, YYYY-MM-DD; ignored when not a date"`
}

func listUnassignedNotes(ctx context.Context, in *listNotesInput) (*out[NotePage], error) {
	r, err := requestOf(ctx)
	if err != nil {
		return nil, err
	}
	pagination := paginationOf(in.Page, in.Limit)

	query := r.db.Model(&models.Note{}).Where("notes.user_id = ? AND contact_id IS NULL", r.userID)
	if from, err := time.Parse("2006-01-02", in.FromDate); err == nil {
		query = query.Where("notes.date >= ?", from)
	}
	if to, err := time.Parse("2006-01-02", in.ToDate); err == nil {
		query = query.Where("notes.date < ?", to.AddDate(0, 0, 1)) // through the whole last day
	}
	if match, ok := ftsMatch(in.Search); ok {
		query = query.Where("notes.id IN (SELECT rowid FROM notes_fts WHERE notes_fts MATCH ?)", match)
	} else if search := strings.ToLower(strings.TrimSpace(in.Search)); search != "" {
		query = query.Where(likeAny("LOWER(content)"), searchArg(search))
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, apperrors.ErrDatabase("Failed to count notes").WithError(err)
	}
	var notes []models.Note
	if err := query.Session(&gorm.Session{}).
		Order("notes.date DESC, notes.id DESC").
		Limit(pagination.Limit).
		Offset(pagination.Offset).
		Find(&notes).Error; err != nil {
		return nil, apperrors.ErrDatabase("Failed to retrieve unassigned notes").WithError(err)
	}
	return respond(NotePage{Notes: toNotes(notes), Total: total, Page: pagination.Page, Limit: pagination.Limit}), nil
}

func updateNote(ctx context.Context, in *idNoteBodyInput) (*out[NoteSaved], error) {
	r, err := requestOf(ctx)
	if err != nil {
		return nil, err
	}
	note, appErr := loadOwned[models.Note](r.db, r.userID, in.ID, "Note")
	if appErr != nil {
		return nil, appErr
	}
	if in.Body.ContactID != nil {
		if _, appErr := loadOwned[models.Contact](r.db, r.userID, *in.Body.ContactID, "Contact"); appErr != nil {
			return nil, appErr
		}
	}
	note.Content = in.Body.Content
	note.Date = in.Body.Date
	note.ContactID = in.Body.ContactID

	// Select names the columns: a plain Updates skips zero values, so a note could
	// never be detached from its contact (contact_id = NULL).
	if err := r.db.Model(&note).Select("content", "date", "contact_id").Updates(&note).Error; err != nil {
		return nil, apperrors.ErrDatabase("Failed to update note").WithError(err)
	}
	go services.TriggerWebhooks(r.db, r.cfg(), r.userID, "note.updated", note)
	return respond(NoteSaved{Message: "Note updated successfully", Note: toNote(note)}), nil
}

func deleteNote(ctx context.Context, in *idInput) (*out[MessageBody], error) {
	r, err := requestOf(ctx)
	if err != nil {
		return nil, err
	}
	note, appErr := loadOwned[models.Note](r.db, r.userID, in.ID, "Note")
	if appErr != nil {
		return nil, appErr
	}
	if err := r.db.Delete(&note).Error; err != nil {
		return nil, apperrors.ErrDatabase("Failed to delete note").WithError(err)
	}
	carddav.RemoveContactPhotoFile(r.cfg().ProfilePhotoDir, note.Image)
	go services.TriggerWebhooks(r.db, r.cfg(), r.userID, "note.deleted", gin.H{"id": note.ID})
	return respond(MessageBody{Message: "Note deleted"}), nil
}
