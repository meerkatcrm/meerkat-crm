package services

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"meerkat/middleware"
	"meerkat/models"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// MaxDataImportSize caps the size of a re-imported data export. It matches the global
// request body limit, so a larger file would be cut off before reaching the handler.
const MaxDataImportSize = 10 * 1024 * 1024

// maxCustomFieldNameLength matches the max=100 on models.CustomFieldNamesInput.
const maxCustomFieldNameLength = 100

// maxDataImportWarnings bounds the warnings returned to the client; the rest are counted.
const maxDataImportWarnings = 50

// Section names written by controllers.ExportData.
const (
	sectionContacts      = "CONTACTS"
	sectionRelationships = "RELATIONSHIPS"
	sectionActivities    = "ACTIVITIES"
	sectionNotes         = "NOTES"
	sectionReminders     = "REMINDERS"
)

var sectionMarkerRe = regexp.MustCompile(`^=== ([A-Z]+) ===$`)

// exportSection is one "=== NAME ===" block of a data export: its header row and data rows.
type exportSection struct {
	headers []string
	rows    [][]string
}

// exportRow gives by-name access to one row of a section.
type exportRow struct {
	index map[string]int
	cells []string
}

func (r exportRow) get(column string) string {
	i, ok := r.index[column]
	if !ok || i >= len(r.cells) {
		return ""
	}
	return r.cells[i]
}

// at returns the cell in column position i ("" when the row is shorter).
func (r exportRow) at(i int) string {
	if i < 0 || i >= len(r.cells) {
		return ""
	}
	return r.cells[i]
}

func (r exportRow) has(column string) bool {
	_, ok := r.index[column]
	return ok
}

func (s *exportSection) each(fn func(rowNumber int, row exportRow) error) error {
	index := make(map[string]int, len(s.headers))
	for i, h := range s.headers {
		if _, taken := index[h]; !taken {
			index[h] = i
		}
	}
	for i, cells := range s.rows {
		if err := fn(i+1, exportRow{index: index, cells: cells}); err != nil {
			return err
		}
	}
	return nil
}

// ParsedDataExport is a data export file split into its sections.
type ParsedDataExport struct {
	sections map[string]*exportSection
}

// ParseDataExport reads a CSV file produced by GET /export (controllers.ExportData).
// It fails when the file has no CONTACTS section, which is how a plain contacts CSV
// (meant for the CSV import wizard) is told apart.
func ParseDataExport(r io.Reader) (*ParsedDataExport, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	// The section markers are written raw, outside the CSV writer; they parse as
	// single-field records, which a data row (always several columns) never is.

	parsed := &ParsedDataExport{sections: map[string]*exportSection{}}
	var current *exportSection
	first := true
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid CSV: %w", err)
		}
		if first && len(record) > 0 {
			record[0] = strings.TrimPrefix(record[0], "\ufeff")
		}
		first = false

		if len(record) == 1 {
			if m := sectionMarkerRe.FindStringSubmatch(strings.TrimSpace(record[0])); m != nil {
				if _, seen := parsed.sections[m[1]]; seen {
					// Two exports pasted together: keeping either copy would silently drop the other.
					return nil, fmt.Errorf("the %s section appears more than once; import one Meerkat data export at a time", m[1])
				}
				current = &exportSection{}
				parsed.sections[m[1]] = current
				continue
			}
		}
		if current == nil {
			return nil, errors.New("this file is not a Meerkat data export")
		}
		if current.headers == nil {
			current.headers = record
			continue
		}
		current.rows = append(current.rows, record)
	}

	contacts := parsed.sections[sectionContacts]
	if contacts == nil || !slices.Contains(contacts.headers, "Firstname") {
		return nil, errors.New("this file is not a Meerkat data export")
	}
	return parsed, nil
}

// Section returns the header row and data rows of one section ("CONTACTS", ...).
func (p *ParsedDataExport) Section(name string) (headers []string, rows [][]string) {
	s := p.section(name)
	return s.headers, s.rows
}

func (p *ParsedDataExport) section(name string) *exportSection {
	if s := p.sections[name]; s != nil {
		return s
	}
	return &exportSection{}
}

// DataImportResult reports what an import of a data export did (or, for a dry run,
// would do).
type DataImportResult struct {
	DryRun            bool                   `json:"dry_run"`
	Contacts          models.DataImportCount `json:"contacts"`
	Relationships     models.DataImportCount `json:"relationships"`
	Activities        models.DataImportCount `json:"activities"`
	Notes             models.DataImportCount `json:"notes"`
	Reminders         models.DataImportCount `json:"reminders"`
	CustomFieldsAdded []string               `json:"custom_fields_added"`
	Warnings          []string               `json:"warnings"`
	MoreWarnings      int                    `json:"more_warnings"`
}

func (r *DataImportResult) warn(format string, args ...any) {
	if len(r.Warnings) >= maxDataImportWarnings {
		r.MoreWarnings++
		return
	}
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// errDryRun rolls back the transaction of a dry run.
var errDryRun = errors.New("dry run")

// dataImporter carries the state of one import inside its transaction.
type dataImporter struct {
	tx     *gorm.DB
	userID uint
	result *DataImportResult

	// contactIDs maps a contact ID from the file to the contact it became for this user.
	contactIDs map[string]uint
	// matched holds the contacts that already existed: only they can already have the
	// relationships, notes and reminders being imported.
	matched map[uint]bool
	// contactsByName maps "Firstname Lastname" as the export writes it to contact IDs,
	// for activities exported before the "Contact IDs" column existed.
	contactsByName map[string][]uint
}

// ImportDataExport adds the data of a parsed export to userID's account. Contacts that
// match an existing contact (same email, name or phone, as the CSV import detects
// duplicates) are not created again and are left unchanged; their relationships, notes,
// reminders and activities are added unless an identical one is already there, so
// importing the same file twice adds nothing the second time.
//
// With dryRun the import runs in a transaction that is rolled back, so the result is
// exactly what a real import would do.
func ImportDataExport(db *gorm.DB, userID uint, parsed *ParsedDataExport, dryRun bool) (*DataImportResult, error) {
	result := &DataImportResult{DryRun: dryRun, CustomFieldsAdded: []string{}, Warnings: []string{}}

	err := db.Transaction(func(tx *gorm.DB) error {
		imp := &dataImporter{
			tx:             tx,
			userID:         userID,
			result:         result,
			contactIDs:     map[string]uint{},
			matched:        map[uint]bool{},
			contactsByName: map[string][]uint{},
		}
		steps := []struct {
			section string
			run     func(*exportSection) error
		}{
			{sectionContacts, imp.importContacts},
			{sectionRelationships, imp.importRelationships},
			{sectionActivities, imp.importActivities},
			{sectionNotes, imp.importNotes},
			{sectionReminders, imp.importReminders},
		}
		for _, step := range steps {
			if err := step.run(parsed.section(step.section)); err != nil {
				return err
			}
		}
		if dryRun {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return nil, err
	}
	return result, nil
}

// parseExportTime reads a timestamp the export wrote with time.RFC3339.
func parseExportTime(value string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	return t, err == nil
}

// sameSecond compares two timestamps at the export's precision (whole seconds).
func sameSecond(a, b time.Time) bool {
	return a.Truncate(time.Second).Equal(b.Truncate(time.Second))
}

// parseFlag reads a true/false cell. An empty cell gives def; a non-empty cell
// ParseBool can't read also gives def, with ok false so the caller can report it.
func parseFlag(cell string, def bool) (value, ok bool) {
	cell = strings.TrimSpace(cell)
	if cell == "" {
		return def, true
	}
	v, err := strconv.ParseBool(cell)
	if err != nil {
		return def, false
	}
	return v, true
}

// customColumn is a custom-field column of the CONTACTS section, read by position: a
// custom field named like a fixed column ("Note") shares its header with that column.
type customColumn struct {
	name  string
	index int
}

// splitTypedList reverses controllers.joinTyped: "type: value; value; type: value".
// A part is read as typed only when the text before the first ": " looks like a type
// label (short, no comma), so a value that merely contains ": " stays whole.
func splitTypedList(cell string) [][2]string {
	var out [][2]string
	for _, part := range strings.Split(cell, "; ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kind, value := "", part
		if i := strings.Index(part, ": "); i > 0 && i <= 30 && !strings.Contains(part[:i], ",") {
			kind, value = strings.TrimSpace(part[:i]), strings.TrimSpace(part[i+2:])
		}
		if value != "" {
			out = append(out, [2]string{kind, value})
		}
	}
	return out
}

// typedItems builds the valid entries of a multi-value field; invalid ones are dropped
// and reported through dropped.
func typedItems[T any](cell string, build func(kind, value string) T, dropped *bool) []T {
	var items []T
	for _, pair := range splitTypedList(cell) {
		item := build(pair[0], pair[1])
		if len(middleware.ValidateStruct(item)) > 0 {
			*dropped = true
			continue
		}
		items = append(items, item)
	}
	return items
}

// droppableContactFields are cleared, rather than failing the whole contact, when the
// value in the file does not pass validation.
var droppableContactFields = map[string]func(*models.Contact){
	"Gender":       func(c *models.Contact) { c.Gender = "" },
	"Email":        func(c *models.Contact) { c.Email = "" },
	"Phone":        func(c *models.Contact) { c.Phone = "" },
	"Birthday":     func(c *models.Contact) { c.Birthday = "" },
	"Anniversary":  func(c *models.Contact) { c.Anniversary = "" },
	"DeceasedDate": func(c *models.Contact) { c.DeceasedDate = "" },
}

// contactFromExportRow builds a contact from one CONTACTS row. It returns the reason
// when the row cannot be imported, and the names of the fields whose values had to be
// dropped.
func contactFromExportRow(userID uint, row exportRow, customColumns []customColumn) (models.Contact, []string, string) {
	text := func(column string) string { return strings.TrimSpace(row.get(column)) }
	contact := models.Contact{
		UserID:             userID,
		Firstname:          text("Firstname"),
		Lastname:           text("Lastname"),
		Nickname:           text("Nickname"),
		Gender:             text("Gender"),
		Email:              text("Email"),
		Phone:              text("Phone"),
		Birthday:           text("Birthday"),
		Address:            text("Address"),
		HowWeMet:           row.get("How We Met"),
		FoodPreference:     row.get("Food Preference"),
		WorkInformation:    row.get("Work Information"),
		ContactInformation: row.get("Contact Information"),
		Note:               row.get("Note"),
		Circles:            CleanCircles(strings.Split(row.get("Circles"), ";")),
		Prefix:             text("Prefix"),
		MiddleName:         text("Middle Name"),
		Suffix:             text("Suffix"),
		Organization:       text("Organization"),
		Department:         text("Department"),
		JobTitle:           text("Job Title"),
		Role:               text("Role"),
		Anniversary:        text("Anniversary"),
		DeceasedDate:       text("Deceased Date"),
	}
	var deceasedOK, archivedOK bool
	contact.IsDeceased, deceasedOK = parseFlag(row.get("Deceased"), false)
	contact.Archived, archivedOK = parseFlag(row.get("Archived"), false)
	if len(contact.Circles) == 0 {
		contact.Circles = nil
	}
	// The validator's max counts characters, not bytes.
	noteShortened := false
	if runes := []rune(contact.Note); len(runes) > models.MaxNoteLength {
		contact.Note = string(runes[:models.MaxNoteLength])
		noteShortened = true
	}

	var dropped []string
	if !deceasedOK {
		dropped = append(dropped, "deceased (not true or false)")
	}
	if !archivedOK {
		dropped = append(dropped, "archived (not true or false)")
	}
	if noteShortened {
		dropped = append(dropped, fmt.Sprintf("note shortened to %d characters", models.MaxNoteLength))
	}
	var bad bool
	contact.Emails = typedItems(row.get("Emails"), func(k, v string) models.ContactEmail { return models.ContactEmail{Type: k, Value: v} }, &bad)
	contact.Phones = typedItems(row.get("Phones"), func(k, v string) models.ContactPhone { return models.ContactPhone{Type: k, Value: v} }, &bad)
	// The export flattens an address to one line, so it comes back as the street.
	contact.Addresses = typedItems(row.get("Addresses"), func(k, v string) models.ContactAddress { return models.ContactAddress{Type: k, Street: v} }, &bad)
	contact.URLs = typedItems(row.get("URLs"), func(k, v string) models.ContactURL { return models.ContactURL{Type: k, Value: v} }, &bad)
	contact.IMPPs = typedItems(row.get("Messaging"), func(k, v string) models.ContactIMPP { return models.ContactIMPP{Type: k, Value: v} }, &bad)
	if bad {
		dropped = append(dropped, "contact details")
	}

	for _, column := range customColumns {
		if value := row.at(column.index); value != "" {
			if contact.CustomFields == nil {
				contact.CustomFields = map[string]string{}
			}
			contact.CustomFields[column.name] = value
		}
	}

	if t, ok := parseExportTime(row.get("Created At")); ok {
		contact.CreatedAt = t
	}
	if t, ok := parseExportTime(row.get("Updated At")); ok {
		contact.UpdatedAt = t
	}

	// BeforeSave derives Email/Phone/Address from the lists, so validate what will be stored.
	if len(contact.Emails) > 0 {
		contact.Email = contact.Emails[0].Value
	}
	if len(contact.Phones) > 0 {
		contact.Phone = contact.Phones[0].Value
	}
	for _, problem := range middleware.ValidateStruct(&contact) {
		clear, ok := droppableContactFields[problem.Field]
		if !ok {
			return contact, dropped, problem.Message
		}
		clear(&contact)
		dropped = append(dropped, problem.Field)
	}
	if problems := middleware.ValidateStruct(&contact); len(problems) > 0 {
		return contact, dropped, problems[0].Message
	}
	return contact, dropped, ""
}

func contactLabel(row exportRow) string {
	return strings.TrimSpace(row.get("Firstname") + " " + row.get("Lastname"))
}

func (imp *dataImporter) importContacts(s *exportSection) error {
	var user models.User
	if err := imp.tx.First(&user, imp.userID).Error; err != nil {
		return err
	}

	// The export writes the fixed columns first, then one per custom field. The first
	// column with a fixed name is that field; a later one with the same name is a custom
	// field that happens to share it.
	var customColumns []customColumn
	fixedSeen := map[string]bool{}
	for i, h := range s.headers {
		if slices.Contains(models.ContactExportHeaders, h) && !fixedSeen[h] {
			fixedSeen[h] = true
			continue
		}
		if strings.TrimSpace(h) == "" || slices.ContainsFunc(customColumns, func(c customColumn) bool { return c.name == h }) {
			continue
		}
		// A custom field name is at most 100 characters (CustomFieldNamesInput), so a
		// longer column could never be shown; leave it out rather than store hidden data.
		if utf8.RuneCountInString(h) > maxCustomFieldNameLength {
			imp.result.warn("Column %q was left out: custom field names are limited to %d characters", h, maxCustomFieldNameLength)
			continue
		}
		customColumns = append(customColumns, customColumn{name: h, index: i})
	}
	names := slices.Clone(user.CustomFieldNames)
	for _, column := range customColumns {
		if !slices.Contains(names, column.name) {
			names = append(names, column.name)
			imp.result.CustomFieldsAdded = append(imp.result.CustomFieldsAdded, column.name)
		}
	}
	if len(imp.result.CustomFieldsAdded) > 0 {
		user.CustomFieldNames = names
		if err := imp.tx.Model(&user).Select("CustomFieldNames").Updates(&user).Error; err != nil {
			return err
		}
	}

	duplicates, err := NewDuplicateIndex(imp.tx, imp.userID)
	if err != nil {
		return err
	}

	return s.each(func(n int, row exportRow) error {
		count := &imp.result.Contacts
		contact, dropped, problem := contactFromExportRow(imp.userID, row, customColumns)
		if problem != "" {
			count.Skipped++
			imp.result.warn("Contact %q (row %d) was skipped: %s", contactLabel(row), n, problem)
			return nil
		}

		var id uint
		if match := duplicates.Detect(contact.Firstname, contact.Lastname, contact.Email, contact.Phone); match != nil {
			id = match.ExistingContactID
			imp.matched[id] = true
			count.Existing++
		} else {
			if err := imp.tx.Create(&contact).Error; err != nil {
				return fmt.Errorf("create contact %q: %w", contactLabel(row), err)
			}
			id = contact.ID
			count.Created++
			if len(dropped) > 0 {
				imp.result.warn("Contact %q (row %d): invalid values were left out (%s)", contactLabel(row), n, strings.Join(dropped, ", "))
			}
		}

		if fileID := strings.TrimSpace(row.get("ID")); fileID != "" {
			imp.contactIDs[fileID] = id
		}
		name := fmt.Sprintf("%s %s", row.get("Firstname"), row.get("Lastname"))
		imp.contactsByName[name] = append(imp.contactsByName[name], id)
		return nil
	})
}

// contactFor resolves the contact a row refers to by its exported ID.
func (imp *dataImporter) contactFor(fileID string) (uint, bool) {
	id, ok := imp.contactIDs[strings.TrimSpace(fileID)]
	return id, ok
}

func (imp *dataImporter) importRelationships(s *exportSection) error {
	return s.each(func(n int, row exportRow) error {
		count := &imp.result.Relationships
		contactID, ok := imp.contactFor(row.get("Contact ID"))
		if !ok {
			count.Skipped++
			imp.result.warn("Relationship %q (row %d) was skipped: its contact is not in the import", row.get("Name"), n)
			return nil
		}
		rel := models.Relationship{
			UserID:    imp.userID,
			ContactID: contactID,
			Name:      strings.TrimSpace(row.get("Name")),
			Type:      strings.TrimSpace(row.get("Type")),
			Gender:    strings.TrimSpace(row.get("Gender")),
			Birthday:  strings.TrimSpace(row.get("Birthday")),
			StartDate: strings.TrimSpace(row.get("Start Date")),
			EndDate:   strings.TrimSpace(row.get("End Date")),
		}
		relatedMissing, relatedMerged := false, false
		if fileID := strings.TrimSpace(row.get("Related Contact ID")); fileID != "" {
			related, ok := imp.contactFor(fileID)
			switch {
			case !ok:
				relatedMissing = true
			case related != contactID || fileID == strings.TrimSpace(row.get("Contact ID")):
				// A link to the relationship's own contact is kept when the file has it so.
				rel.RelatedContactID = &related
			default:
				// Two different contacts in the file matched the same contact here; linking
				// would point the relationship back at its own contact.
				relatedMerged = true
			}
		}
		if problems := middleware.ValidateStruct(&rel); len(problems) > 0 {
			count.Skipped++
			imp.result.warn("Relationship %q (row %d) was skipped: %s", rel.Name, n, problems[0].Message)
			return nil
		}

		if imp.matched[contactID] {
			var existing int64
			if err := imp.tx.Model(&models.Relationship{}).
				Where("user_id = ? AND contact_id = ? AND name = ? AND type = ?", imp.userID, contactID, rel.Name, rel.Type).
				Count(&existing).Error; err != nil {
				return err
			}
			if existing > 0 {
				count.Existing++
				return nil
			}
		}

		if t, ok := parseExportTime(row.get("Created At")); ok {
			rel.CreatedAt = t
		}
		if t, ok := parseExportTime(row.get("Updated At")); ok {
			rel.UpdatedAt = t
		}
		if err := imp.tx.Create(&rel).Error; err != nil {
			return fmt.Errorf("create relationship: %w", err)
		}
		count.Created++
		if relatedMissing {
			imp.result.warn("Relationship %q (row %d): its related contact is not in the import and was not linked", rel.Name, n)
		}
		if relatedMerged {
			imp.result.warn("Relationship %q (row %d): its related contact matched the same existing contact as the relationship's own contact, so it was not linked", rel.Name, n)
		}
		return nil
	})
}

// activityContacts resolves the contacts of an ACTIVITIES row: by ID when the export has
// the "Contact IDs" column, by name for older exports.
func (imp *dataImporter) activityContacts(row exportRow) ([]uint, int) {
	var ids []uint
	missing := 0
	add := func(id uint) {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if row.has("Contact IDs") {
		for _, fileID := range strings.Split(row.get("Contact IDs"), ";") {
			if strings.TrimSpace(fileID) == "" {
				continue
			}
			if id, ok := imp.contactFor(fileID); ok {
				add(id)
			} else {
				missing++
			}
		}
		return ids, missing
	}
	for _, name := range strings.Split(row.get("Contact Names"), "; ") {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if matches := imp.contactsByName[name]; len(matches) > 0 {
			add(matches[0])
		} else {
			missing++
		}
	}
	return ids, missing
}

func (imp *dataImporter) importActivities(s *exportSection) error {
	var rows []models.Activity
	if err := imp.tx.Where("user_id = ?", imp.userID).Select("id", "title", "date").Find(&rows).Error; err != nil {
		return err
	}
	// Keyed like sameSecond compares, so each row is one lookup instead of a scan.
	type activityKey struct {
		title  string
		second int64
	}
	keyOf := func(title string, date time.Time) activityKey {
		return activityKey{title, date.Truncate(time.Second).Unix()}
	}
	existing := make(map[activityKey]bool, len(rows))
	for _, a := range rows {
		existing[keyOf(a.Title, a.Date)] = true
	}

	return s.each(func(n int, row exportRow) error {
		count := &imp.result.Activities
		date, ok := parseExportTime(row.get("Date"))
		activity := models.Activity{
			UserID:      imp.userID,
			Title:       strings.TrimSpace(row.get("Title")),
			Description: row.get("Description"),
			Location:    strings.TrimSpace(row.get("Location")),
			Date:        date,
		}
		if !ok {
			count.Skipped++
			imp.result.warn("Activity %q (row %d) was skipped: its date is missing or invalid", activity.Title, n)
			return nil
		}
		if problems := middleware.ValidateStruct(&activity); len(problems) > 0 {
			count.Skipped++
			imp.result.warn("Activity %q (row %d) was skipped: %s", activity.Title, n, problems[0].Message)
			return nil
		}
		if existing[keyOf(activity.Title, activity.Date)] {
			count.Existing++
			return nil
		}

		contactIDs, missing := imp.activityContacts(row)
		if missing > 0 {
			imp.result.warn("Activity %q (row %d): %d of its contacts are not in the import and were not linked", activity.Title, n, missing)
		}
		if t, ok := parseExportTime(row.get("Created At")); ok {
			activity.CreatedAt = t
		}
		if t, ok := parseExportTime(row.get("Updated At")); ok {
			activity.UpdatedAt = t
		}
		if err := imp.tx.Create(&activity).Error; err != nil {
			return fmt.Errorf("create activity: %w", err)
		}
		if len(contactIDs) > 0 {
			contacts := make([]models.Contact, len(contactIDs))
			for i, id := range contactIDs {
				contacts[i].ID = id
			}
			if err := imp.tx.Model(&activity).Omit("Contacts.*").Association("Contacts").Append(contacts); err != nil {
				return fmt.Errorf("link activity contacts: %w", err)
			}
		}
		count.Created++
		return nil
	})
}

func (imp *dataImporter) importNotes(s *exportSection) error {
	return s.each(func(n int, row exportRow) error {
		count := &imp.result.Notes
		note := models.Note{UserID: imp.userID, Content: row.get("Content")}
		// A note without a contact is a journal entry; one whose contact is missing
		// from the file is skipped.
		standalone := strings.TrimSpace(row.get("Contact ID")) == ""
		contactID, ok := imp.contactFor(row.get("Contact ID"))
		if !ok && !standalone {
			count.Skipped++
			imp.result.warn("Note (row %d) was skipped: its contact is not in the import", n)
			return nil
		}
		if !standalone {
			note.ContactID = &contactID
		}
		if note.Date, ok = parseExportTime(row.get("Date")); !ok {
			count.Skipped++
			imp.result.warn("Note (row %d) was skipped: its date is missing or invalid", n)
			return nil
		}
		// Not ValidateStruct: it would descend into the empty Note.Contact.
		if strings.TrimSpace(note.Content) == "" || utf8.RuneCountInString(note.Content) > models.MaxNoteLength {
			count.Skipped++
			imp.result.warn("Note (row %d) was skipped: its content is empty or longer than %d characters", n, models.MaxNoteLength)
			return nil
		}

		if standalone || imp.matched[contactID] {
			query := imp.tx.Where("user_id = ? AND content = ?", imp.userID, note.Content)
			if standalone {
				query = query.Where("contact_id IS NULL")
			} else {
				query = query.Where("contact_id = ?", contactID)
			}
			var existing []models.Note
			if err := query.Select("id", "date").Find(&existing).Error; err != nil {
				return err
			}
			if slices.ContainsFunc(existing, func(e models.Note) bool { return sameSecond(e.Date, note.Date) }) {
				count.Existing++
				return nil
			}
		}

		if t, ok := parseExportTime(row.get("Created At")); ok {
			note.CreatedAt = t
		}
		if t, ok := parseExportTime(row.get("Updated At")); ok {
			note.UpdatedAt = t
		}
		if err := imp.tx.Omit("Contact").Create(&note).Error; err != nil {
			return fmt.Errorf("create note: %w", err)
		}
		count.Created++
		return nil
	})
}

func (imp *dataImporter) importReminders(s *exportSection) error {
	return s.each(func(n int, row exportRow) error {
		count := &imp.result.Reminders
		reminder := models.Reminder{
			UserID:     imp.userID,
			Message:    row.get("Message"),
			Recurrence: strings.TrimSpace(row.get("Recurrence")),
		}
		contactID, ok := imp.contactFor(row.get("Contact ID"))
		if !ok {
			count.Skipped++
			imp.result.warn("Reminder %q (row %d) was skipped: its contact is not in the import", reminder.Message, n)
			return nil
		}
		reminder.ContactID = &contactID
		if reminder.RemindAt, ok = parseExportTime(row.get("Remind At")); !ok {
			count.Skipped++
			imp.result.warn("Reminder %q (row %d) was skipped: its date is missing or invalid", reminder.Message, n)
			return nil
		}
		// Fallbacks are the column defaults; an unreadable value is reported.
		var unreadable []string
		flag := func(column string, def bool) bool {
			v, ok := parseFlag(row.get(column), def)
			if !ok {
				unreadable = append(unreadable, column)
			}
			return v
		}
		byMail := flag("By Mail", false)
		reminder.ByMail = &byMail
		reoccur := flag("Reoccur From Completion", true)
		reminder.ReoccurFromCompletion = &reoccur
		reminder.Completed = flag("Completed", false)
		if lastSent, ok := parseExportTime(row.get("Last Sent")); ok {
			reminder.LastSent = &lastSent
		}
		// Whether the current occurrence was already emailed, so the new account doesn't
		// send it again. Older exports have no Email Sent column: there the email went out
		// for this occurrence if it was sent on or after the reminder's date (moving a
		// reminder to a later date resets email_sent and leaves Last Sent behind).
		if row.has("Email Sent") {
			reminder.EmailSent = flag("Email Sent", false)
		} else {
			reminder.EmailSent = reminder.LastSent != nil && !reminder.LastSent.Before(reminder.RemindAt)
		}
		if problems := middleware.ValidateStruct(&reminder); len(problems) > 0 {
			count.Skipped++
			imp.result.warn("Reminder %q (row %d) was skipped: %s", reminder.Message, n, problems[0].Message)
			return nil
		}

		if imp.matched[contactID] {
			var existing []models.Reminder
			if err := imp.tx.Where("user_id = ? AND contact_id = ? AND message = ?", imp.userID, contactID, reminder.Message).
				Select("id", "remind_at").Find(&existing).Error; err != nil {
				return err
			}
			if slices.ContainsFunc(existing, func(e models.Reminder) bool { return sameSecond(e.RemindAt, reminder.RemindAt) }) {
				count.Existing++
				return nil
			}
		}

		if t, ok := parseExportTime(row.get("Created At")); ok {
			reminder.CreatedAt = t
		}
		if t, ok := parseExportTime(row.get("Updated At")); ok {
			reminder.UpdatedAt = t
		}
		// Create with explicit false values (ByMail, Completed) instead of column defaults.
		if err := imp.tx.Omit("Contact").Create(&reminder).Error; err != nil {
			return fmt.Errorf("create reminder: %w", err)
		}
		count.Created++
		if len(unreadable) > 0 {
			imp.result.warn("Reminder %q (row %d): %s not true or false, the default was used", reminder.Message, n, strings.Join(unreadable, ", "))
		}
		return nil
	})
}
