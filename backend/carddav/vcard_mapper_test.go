package carddav

import (
	"bytes"
	"meerkat/models"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-vcard"
	"github.com/stretchr/testify/assert"
)

// TestVCardRoundTrip verifies that the multi-valued and structured vCard fields
// survive a Contact -> vCard -> Contact round trip without data loss.
func TestVCardRoundTrip(t *testing.T) {
	original := &models.Contact{
		Firstname:  "Ada",
		Lastname:   "Lovelace",
		MiddleName: "Augusta",
		Prefix:     "Dr.",
		Suffix:     "PhD",
		Nickname:   "Ada",
		Emails: []models.ContactEmail{
			{Type: "home", Value: "ada@home.example"},
			{Type: "work", Value: "ada@work.example"},
		},
		Phones: []models.ContactPhone{
			{Type: "cell", Value: "+15551234567"},
			{Type: "work", Value: "+15557654321"},
		},
		Addresses: []models.ContactAddress{
			{Type: "home", Street: "1 Analytical Way", City: "London", Region: "ENG", Postal: "EC1", Country: "UK"},
		},
		URLs:         []models.ContactURL{{Type: "home", Value: "https://example.com"}},
		IMPPs:        []models.ContactIMPP{{Type: "telegram", Value: "@ada"}},
		Organization: "Analytical Engines Ltd",
		Department:   "R&D",
		JobTitle:     "Mathematician",
		Role:         "Pioneer",
		Birthday:     "1815-12-10",
		Anniversary:  "1835-07-08",
		Circles:      []string{"friends", "history"},
	}

	card := ContactToVCard(original, "")
	got, _, _, _ := VCardToContact(card, nil)

	if got.Firstname != original.Firstname || got.Lastname != original.Lastname {
		t.Errorf("name mismatch: got %q %q", got.Firstname, got.Lastname)
	}
	if got.MiddleName != "Augusta" || got.Prefix != "Dr." || got.Suffix != "PhD" {
		t.Errorf("structured name parts lost: %+v", got)
	}
	if len(got.Emails) != 2 {
		t.Fatalf("expected 2 emails, got %d: %+v", len(got.Emails), got.Emails)
	}
	if got.Emails[0].Value != "ada@home.example" || got.Emails[0].Type != "home" {
		t.Errorf("email[0] mismatch: %+v", got.Emails[0])
	}
	if got.Emails[1].Type != "work" {
		t.Errorf("email[1] type lost: %+v", got.Emails[1])
	}
	if got.Email != "ada@home.example" {
		t.Errorf("primary email scalar not set: %q", got.Email)
	}
	if len(got.Phones) != 2 || got.Phones[0].Type != "cell" {
		t.Errorf("phones mismatch: %+v", got.Phones)
	}
	if len(got.Addresses) != 1 {
		t.Fatalf("expected 1 address, got %d", len(got.Addresses))
	}
	a := got.Addresses[0]
	if a.Street != "1 Analytical Way" || a.City != "London" || a.Region != "ENG" || a.Postal != "EC1" || a.Country != "UK" {
		t.Errorf("address structure lost: %+v", a)
	}
	if len(got.URLs) != 1 || got.URLs[0].Value != "https://example.com" {
		t.Errorf("url lost: %+v", got.URLs)
	}
	if len(got.IMPPs) != 1 || got.IMPPs[0].Value != "@ada" || got.IMPPs[0].Type != "telegram" {
		t.Errorf("impp lost: %+v", got.IMPPs)
	}
	if got.Organization != "Analytical Engines Ltd" || got.Department != "R&D" {
		t.Errorf("org/department lost: org=%q dept=%q", got.Organization, got.Department)
	}
	if got.JobTitle != "Mathematician" || got.Role != "Pioneer" {
		t.Errorf("title/role lost: title=%q role=%q", got.JobTitle, got.Role)
	}
	if got.Anniversary != "1835-07-08" {
		t.Errorf("anniversary lost: %q", got.Anniversary)
	}
}

// TestVCardUnmappedPreserved verifies an unknown property is kept in VCardExtra.
func TestVCardUnmappedPreserved(t *testing.T) {
	card := make(vcard.Card)
	card.SetValue(vcard.FieldFormattedName, "Test Person")
	card.SetValue(vcard.FieldName, "Person;Test;;;")
	card.Add("X-CUSTOM-PROP", &vcard.Field{Value: "keep-me"})

	got, _, _, _ := VCardToContact(card, nil)
	if got.VCardExtra == "" {
		t.Fatal("expected VCardExtra to capture unmapped X-CUSTOM-PROP")
	}

	// Re-export and confirm the unmapped property is restored.
	out := ContactToVCard(got, "")
	if v := out.Value("X-CUSTOM-PROP"); v != "keep-me" {
		t.Errorf("unmapped property not restored on export: %q", v)
	}
}

// TestNoDuplicateFromStaleExtra verifies that a property which now maps to a column
// is not emitted twice when a stale copy still lingers in vcard_extra (the situation
// migration 000021 cleans up, with this export guard as the safety net).
func TestNoDuplicateFromStaleExtra(t *testing.T) {
	c := &models.Contact{
		Firstname: "Stale",
		Lastname:  "Extra",
		// New column has the website…
		URLs: []models.ContactURL{{Type: "home", Value: "https://example.com"}},
		// …and a leftover pre-upgrade copy still sits in vcard_extra.
		VCardExtra: `{"properties":{"URL":[{"Value":"https://example.com","Params":{},"Group":""}],"X-CUSTOM":[{"Value":"keep","Params":{},"Group":""}]}}`,
	}

	card := ContactToVCard(c, "")

	if got := len(card[vcard.FieldURL]); got != 1 {
		t.Errorf("expected exactly 1 URL on export, got %d: %v", got, card[vcard.FieldURL])
	}
	// Genuinely unmapped properties must still be restored.
	if v := card.Value("X-CUSTOM"); v != "keep" {
		t.Errorf("unmapped X-CUSTOM should still be restored, got %q", v)
	}
}

// TestLegacyScalarFallback verifies a contact with only the legacy scalar fields
// still exports valid EMAIL/TEL/ADR entries.
func TestLegacyScalarFallback(t *testing.T) {
	c := &models.Contact{
		Firstname: "Legacy",
		Lastname:  "User",
		Email:     "legacy@example.com",
		Phone:     "+15550000000",
		Address:   "10 Old Street",
	}
	card := ContactToVCard(c, "")
	if v := card.Value(vcard.FieldEmail); v != "legacy@example.com" {
		t.Errorf("legacy email not exported: %q", v)
	}
	if v := card.Value(vcard.FieldTelephone); v != "+15550000000" {
		t.Errorf("legacy phone not exported: %q", v)
	}
	if len(card.Addresses()) == 0 {
		t.Error("legacy address not exported")
	}
}

// TestStructuredSemicolonRoundTrip verifies that a literal ";" inside ORG/ADR
// components survives a round trip instead of leaking into the next component.
func TestStructuredSemicolonRoundTrip(t *testing.T) {
	original := &models.Contact{
		Firstname:    "Semi",
		Lastname:     "Colon",
		Organization: "Smith; Jones & Co",
		Department:   "R&D; Labs",
		Addresses: []models.ContactAddress{
			{Type: "work", Street: "1 Main St; Suite 2", City: "Town; ville", Region: "RE", Postal: "12345", Country: "UK"},
		},
	}

	card := ContactToVCard(original, "")
	got, _, _, _ := VCardToContact(card, nil)

	if got.Organization != original.Organization {
		t.Errorf("organization corrupted: got %q want %q", got.Organization, original.Organization)
	}
	if got.Department != original.Department {
		t.Errorf("department corrupted: got %q want %q", got.Department, original.Department)
	}
	if len(got.Addresses) != 1 {
		t.Fatalf("expected 1 address, got %d", len(got.Addresses))
	}
	a := got.Addresses[0]
	if a.Street != "1 Main St; Suite 2" || a.City != "Town; ville" {
		t.Errorf("address component with ';' corrupted: %+v", a)
	}
}

// TestComponentBackslashRoundTrip verifies a literal backslash in a structured
// component is not mangled by our escaping interacting with go-vcard's own.
func TestComponentBackslashRoundTrip(t *testing.T) {
	original := &models.Contact{
		Firstname:    "Back",
		Lastname:     "Slash",
		Organization: `Path\To\Co`,
	}
	got, _, _, _ := VCardToContact(ContactToVCard(original, ""), nil)
	if got.Organization != `Path\To\Co` {
		t.Errorf("backslash corrupted: got %q", got.Organization)
	}
}

// TestCustomLabelRoundTrip verifies that a user-defined label (not a standard
// vCard TYPE token) is emitted as a grouped X-ABLabel and read back intact.
func TestCustomLabelRoundTrip(t *testing.T) {
	original := &models.Contact{
		Firstname: "Custom",
		Lastname:  "Label",
		Emails:    []models.ContactEmail{{Type: "School", Value: "c@school.example"}},
		Phones:    []models.ContactPhone{{Type: "Ski Cabin", Value: "+15550000000"}},
		Addresses: []models.ContactAddress{{Type: "Holiday Home", Street: "1 Beach Rd", City: "Nice"}},
	}

	card := ContactToVCard(original, "")

	// The custom label must travel as a grouped X-ABLabel, not a TYPE token.
	emailField := card[vcard.FieldEmail][0]
	if emailField.Group == "" {
		t.Fatalf("custom-labelled email was not assigned a property group")
	}
	if emailField.Params.Get(vcard.ParamType) != "INTERNET" {
		t.Errorf("email base TYPE should remain INTERNET, got %q", emailField.Params.Get(vcard.ParamType))
	}
	labels := card[fieldABLabel]
	if len(labels) != 3 {
		t.Fatalf("expected 3 X-ABLabel entries, got %d", len(labels))
	}

	got, _, _, _ := VCardToContact(card, nil)
	if len(got.Emails) != 1 || got.Emails[0].Type != "School" {
		t.Errorf("custom email label lost: %+v", got.Emails)
	}
	if len(got.Phones) != 1 || got.Phones[0].Type != "Ski Cabin" {
		t.Errorf("custom phone label lost: %+v", got.Phones)
	}
	if len(got.Addresses) != 1 || got.Addresses[0].Type != "Holiday Home" {
		t.Errorf("custom address label lost: %+v", got.Addresses)
	}

	// X-ABLabel is a mapped field, so it must not leak into VCardExtra (which
	// would duplicate / orphan it on the next export).
	if got.VCardExtra != "" {
		t.Errorf("X-ABLabel should not be stored in VCardExtra, got %q", got.VCardExtra)
	}
}

// TestApplePseudoLabelImport verifies that Apple's "_$!<...>!$_" pseudo-labels
// are normalized back to our standard tokens on import.
func TestApplePseudoLabelImport(t *testing.T) {
	card := vcard.Card{}
	card.SetValue(vcard.FieldFormattedName, "Apple User")
	card.Add(vcard.FieldTelephone, &vcard.Field{Group: "item1", Value: "+15551112222"})
	card.Add(fieldABLabel, &vcard.Field{Group: "item1", Value: "_$!<Mobile>!$_"})

	got, _, _, _ := VCardToContact(card, nil)
	if len(got.Phones) != 1 || got.Phones[0].Type != "cell" {
		t.Errorf("apple pseudo-label not normalized to cell: %+v", got.Phones)
	}
}

// verifies that REV describes Meerkat's own last edit. A REV arriving with an import belongs to whichever client
// wrote it; preserving and re-emitting it would advertise a stale revision and let peers discard newer local edits as older.
func TestRevisionReflectsLocalModification(t *testing.T) {
	card := vcard.Card{}
	card.SetValue(vcard.FieldVersion, "3.0")
	card.SetValue(vcard.FieldUID, "uid-rev")
	card.SetValue(vcard.FieldFormattedName, "Rev Person")
	card.SetValue(vcard.FieldName, "Person;Rev;;;")
	card.SetValue(vcard.FieldRevision, "20200101T000000Z")

	contact, _, _, _ := VCardToContact(card, nil)
	if strings.Contains(contact.VCardExtra, "REV") {
		t.Errorf("imported REV must not be kept in VCardExtra, got %q", contact.VCardExtra)
	}

	modified := time.Date(2026, 7, 22, 10, 30, 0, 0, time.UTC)
	contact.UpdatedAt = modified

	exported := ContactToVCard(contact, "")
	rev, ok := ParseRevision(exported)
	if !ok {
		t.Fatalf("exported REV is not parseable: %q", exported.Value(vcard.FieldRevision))
	}
	if !rev.Equal(modified) {
		t.Errorf("REV = %v, want the contact's UpdatedAt %v", rev, modified)
	}
	if got := len(exported[vcard.FieldRevision]); got != 1 {
		t.Errorf("expected exactly one REV property, got %d", got)
	}
}

// TestRevisionEncoding pins the exact bytes we put on the wire: the extended
// form RFC 2426 specifies for vCard 3.0, normalized to UTC. A non-UTC timestamp
// would otherwise be written with a "Z" suffix against the wrong instant.
func TestRevisionEncoding(t *testing.T) {
	zone := time.FixedZone("UTC+5", 5*60*60)
	contact := &models.Contact{Firstname: "Zone", Lastname: "Person", VCardUID: "uid-zone"}
	contact.UpdatedAt = time.Date(2026, 7, 22, 15, 0, 0, 0, zone)

	exported := ContactToVCard(contact, "")
	if got, want := exported.Value(vcard.FieldRevision), "2026-07-22T10:00:00Z"; got != want {
		t.Errorf("REV = %q, want %q (RFC 2426 extended form, same instant in UTC)", got, want)
	}
}

// TestRevisionOmittedForUnsavedContact keeps ContactToVCard from inventing a
// revision for a contact that was never persisted.
func TestRevisionOmittedForUnsavedContact(t *testing.T) {
	contact := &models.Contact{Firstname: "New", Lastname: "Person", VCardUID: "uid-new"}

	exported := ContactToVCard(contact, "")
	if raw := exported.Value(vcard.FieldRevision); raw != "" {
		t.Errorf("expected no REV for an unsaved contact, got %q", raw)
	}
}

// pins the encodings REV actually arrives in. go-vcard's own Card.Revision accepts only the last of these,
// so relying on it made conflict resolution silently ignore the revision sent by every Apple client and by Nextcloud's web UI.
func TestParseRevisionAcceptsRealWorldFormats(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want time.Time
	}{
		{"apple ios 17", "2023-10-04T12:07:13Z", time.Date(2023, 10, 4, 12, 7, 13, 0, time.UTC)},
		{"apple ios 5", "2011-11-07T09:28:43Z", time.Date(2011, 11, 7, 9, 28, 43, 0, time.UTC)},
		{"apple icloud web", "2023-01-03T17:21:11Z", time.Date(2023, 1, 3, 17, 21, 11, 0, time.UTC)},
		{"nextcloud toISOString", "2014-02-19T02:24:26.123Z", time.Date(2014, 2, 19, 2, 24, 26, 123000000, time.UTC)},
		{"rfc 2426 example", "1995-10-31T22:27:10Z", time.Date(1995, 10, 31, 22, 27, 10, 0, time.UTC)},
		{"rfc 2426 date only", "1997-11-15", time.Date(1997, 11, 15, 0, 0, 0, 0, time.UTC)},
		{"extended with offset", "2023-10-04T14:07:13+02:00", time.Date(2023, 10, 4, 12, 7, 13, 0, time.UTC)},
		{"basic utc (vcard 4.0)", "20231004T120713Z", time.Date(2023, 10, 4, 12, 7, 13, 0, time.UTC)},
		{"basic with offset", "20231004T140713+0200", time.Date(2023, 10, 4, 12, 7, 13, 0, time.UTC)},
		{"basic without zone", "20231004T120713", time.Date(2023, 10, 4, 12, 7, 13, 0, time.UTC)},
		{"basic date only", "19971115", time.Date(1997, 11, 15, 0, 0, 0, 0, time.UTC)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := vcard.Card{}
			card.SetValue(vcard.FieldRevision, tc.raw)

			got, ok := ParseRevision(card)
			if !ok {
				t.Fatalf("ParseRevision(%q) reported no usable revision", tc.raw)
			}
			if !got.Equal(tc.want) {
				t.Errorf("ParseRevision(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseRevisionRejectsUnusableValues(t *testing.T) {
	cases := map[string]string{
		"absent":     "",
		"whitespace": "   ",
		"garbage":    "last tuesday",
		"zero time":  "00010101T000000Z",
		"partial":    "2023-10",
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			card := vcard.Card{}
			if raw != "" {
				card.SetValue(vcard.FieldRevision, raw)
			}
			if got, ok := ParseRevision(card); ok {
				t.Errorf("ParseRevision(%q) = %v, want no usable revision", raw, got)
			}
		})
	}
}

// TestParseRevisionReadsOurOwnOutput keeps the writer and the reader in step:
// whatever ContactToVCard emits must be interpretable by the code that resolves
// conflicts, so changing the emitted encoding cannot silently blind us.
func TestParseRevisionReadsOurOwnOutput(t *testing.T) {
	modified := time.Date(2026, 7, 22, 10, 30, 0, 0, time.UTC)
	contact := &models.Contact{Firstname: "Round", Lastname: "Trip", VCardUID: "uid-roundtrip"}
	contact.UpdatedAt = modified

	got, ok := ParseRevision(ContactToVCard(contact, ""))
	if !ok {
		t.Fatal("ParseRevision could not read the REV that ContactToVCard wrote")
	}
	if !got.Equal(modified) {
		t.Errorf("round-tripped REV = %v, want %v", got, modified)
	}
}

// A PUT replaces the card, so a property the client removed must be cleared.
func TestVCardToContact_UpdateClearsRemovedProperties(t *testing.T) {
	existing := &models.Contact{
		Firstname: "Ada", Lastname: "Lovelace", Nickname: "Countess",
		Birthday: "1815-12-10", Email: "ada@example.com", Phone: "+441234567",
		Emails:       []models.ContactEmail{{Type: "home", Value: "ada@example.com"}},
		Phones:       []models.ContactPhone{{Type: "cell", Value: "+441234567"}},
		URLs:         []models.ContactURL{{Type: "home", Value: "https://example.com"}},
		Organization: "Analytical Engines", JobTitle: "Programmer",
		Circles: []string{"Family"}, Gender: "female", Anniversary: "1835-07-08",
		HowWeMet: "At a lecture", About: "Met at the Royal Society",
	}

	card := vcard.Card{}
	card.SetValue(vcard.FieldVersion, "3.0")
	card.SetValue(vcard.FieldFormattedName, "Ada Lovelace")
	card.SetName(&vcard.Name{GivenName: "Ada", FamilyName: "Lovelace"})
	card.Add(vcard.FieldEmail, &vcard.Field{Value: "ada@new.example"})

	contact, _, _, _ := VCardToContact(card, existing)

	assert.Equal(t, "ada@new.example", contact.Email)
	assert.Len(t, contact.Emails, 1)
	assert.Empty(t, contact.Nickname)
	assert.Empty(t, contact.Birthday)
	assert.Empty(t, contact.Phone)
	assert.Empty(t, contact.Phones)
	assert.Empty(t, contact.URLs)
	assert.Empty(t, contact.Organization)
	assert.Empty(t, contact.JobTitle)
	assert.Empty(t, contact.About, "a note removed on the phone is removed here too")

	// Not every client can round-trip these, and Meerkat-only fields are not in a card at all.
	assert.Equal(t, []string{"Family"}, contact.Circles)
	assert.Equal(t, "female", contact.Gender)
	assert.Equal(t, "1835-07-08", contact.Anniversary)
	assert.Equal(t, "At a lecture", contact.HowWeMet)
}

func TestContactToVCard_ExportsGender(t *testing.T) {
	card := ContactToVCard(&models.Contact{Firstname: "Ada", Gender: "female"}, "")
	assert.Equal(t, "F", card.Value(vcard.FieldGender))

	back, _, _, _ := VCardToContact(card, nil)
	assert.Equal(t, "female", back.Gender)
}

// decodeVCF parses raw vCard text the way CardDAV PUTs and VCF imports do, so the
// tests below exercise go-vcard's real unescaping rather than hand-built fields.
func decodeVCF(t *testing.T, raw string) vcard.Card {
	t.Helper()
	card, err := vcard.NewDecoder(strings.NewReader(strings.ReplaceAll(raw, "\n", "\r\n"))).Decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return card
}

// Issue #290: NOTE was unmapped, so it never reached Meerkat and edits made in
// Meerkat could not reach the phone.
func TestVCardToContact_MapsNoteToAbout(t *testing.T) {
	card := decodeVCF(t, `BEGIN:VCARD
VERSION:3.0
FN:Jane Doe
N:Doe;Jane;;;
NOTE:Met at a wedding\, Lisbon 2019.\nAllergic to nuts\; prefers tea.
END:VCARD
`)

	got, _, _, _ := VCardToContact(card, nil)

	assert.Equal(t, "Met at a wedding, Lisbon 2019.\nAllergic to nuts; prefers tea.", got.About)
	assert.NotContains(t, got.VCardExtra, "NOTE", "a mapped property must not also be kept in vcard_extra")
}

func TestVCardToContact_JoinsSeveralNotes(t *testing.T) {
	card := decodeVCF(t, `BEGIN:VCARD
VERSION:4.0
FN:Jane Doe
NOTE:First
NOTE:
NOTE:Second
END:VCARD
`)

	got, _, _, _ := VCardToContact(card, nil)

	assert.Equal(t, "First\n\nSecond", got.About)
}

func TestVCardToContact_CapsAboutAtColumnLimit(t *testing.T) {
	card := vcard.Card{}
	card.SetValue(vcard.FieldFormattedName, "Jane Doe")
	card.SetValue(vcard.FieldNote, strings.Repeat("ä", maxAboutLength+10))

	got, _, _, _ := VCardToContact(card, nil)

	assert.Equal(t, maxAboutLength, len([]rune(got.About)))
}

func TestContactToVCard_AboutRoundTripsThroughText(t *testing.T) {
	original := &models.Contact{Firstname: "Jane", Lastname: "Doe", About: "Line one, with a comma.\nLine two; with a semicolon."}

	var buf bytes.Buffer
	if err := vcard.NewEncoder(&buf).Encode(ContactToVCard(original, "")); err != nil {
		t.Fatalf("encode: %v", err)
	}
	assert.Len(t, ContactToVCard(original, "")[vcard.FieldNote], 1)

	card, err := vcard.NewDecoder(&buf).Decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	back, _, _, _ := VCardToContact(card, nil)
	assert.Equal(t, original.About, back.About)
}

func TestContactToVCard_NoNoteWhenAboutEmpty(t *testing.T) {
	card := ContactToVCard(&models.Contact{Firstname: "Jane"}, "")
	assert.Nil(t, card[vcard.FieldNote])
}

// A NOTE still stored in vcard_extra (before migration 000034 moved it) must not be
// exported next to the column value.
func TestContactToVCard_IgnoresStaleNoteInExtra(t *testing.T) {
	c := &models.Contact{
		Firstname:  "Jane",
		About:      "current",
		VCardExtra: `{"properties":{"NOTE":[{"Value":"stale","Params":{},"Group":""}]}}`,
	}

	card := ContactToVCard(c, "")

	if assert.Len(t, card[vcard.FieldNote], 1) {
		assert.Equal(t, "current", card[vcard.FieldNote][0].Value)
	}
}
