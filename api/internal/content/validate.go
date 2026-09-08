package content

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// normalize validates data against key's O-19 shape and, on success,
// returns the canonical JSON to store: the validated Go type
// (gen.ContentHero etc.) marshalled back out, not the caller's raw map —
// so a required field's surrounding whitespace, trimmed here the same way
// shop.Handler.UpdateShop trims name, is what actually lands in
// content_blocks.data, and an unknown property never reaches storage
// (every O-19 schema is additionalProperties: false). fields is non-nil
// only when validation failed; its values are the docs/05-API.md §
// Conventions O-12 vocabulary (required, invalid, too_long) plus "invalid"
// for an unrecognized property, keyed by the offending field's JSON name
// (nested under "days[<index>].<field>" for an hours row).
func normalize(key gen.ContentKey, data map[string]interface{}) (json.RawMessage, map[string]string) {
	var typed interface{}
	var fields map[string]string

	switch key {
	case gen.Hero:
		typed, fields = validateHero(data)
	case gen.About:
		typed, fields = validateAbout(data)
	case gen.Hours:
		typed, fields = validateHours(data)
	case gen.Contacts:
		typed, fields = validateContacts(data)
	case gen.Social:
		typed, fields = validateSocial(data)
	case gen.Seo:
		typed, fields = validateSeo(data)
	default:
		// Unreachable: Service.Upsert checks key.Valid() before calling
		// normalize.
		return nil, map[string]string{"key": "invalid"}
	}
	if len(fields) > 0 {
		return nil, fields
	}

	raw, err := json.Marshal(typed)
	if err != nil {
		// Unreachable: every typed value above is one of the six plain
		// gen.Content* structs, none of which can fail to marshal.
		return nil, map[string]string{"data": "invalid"}
	}
	return raw, nil
}

// unknownFields flags every key in data that is not in known as
// "invalid" — the additionalProperties: false half of each O-19 schema
// (JSON Schema's own vocabulary has no separate word for it, and O-12's
// four-word vocabulary doesn't either, so an unrecognized property gets
// the same word an out-of-type value would).
func unknownFields(data map[string]interface{}, known map[string]struct{}, fields map[string]string) {
	for k := range data {
		if _, ok := known[k]; !ok {
			fields[k] = "invalid"
		}
	}
}

// requiredString extracts and trims a required, non-blank string field
// bounded by maxLen runes.
func requiredString(data map[string]interface{}, name string, maxLen int, fields map[string]string) string {
	v, present := data[name]
	if !present {
		fields[name] = "required"
		return ""
	}
	s, ok := v.(string)
	if !ok {
		fields[name] = "invalid"
		return ""
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		fields[name] = "required"
		return ""
	}
	if utf8.RuneCountInString(trimmed) > maxLen {
		fields[name] = "too_long"
		return ""
	}
	return trimmed
}

// optionalString extracts an optional string field bounded by maxLen
// runes; absent is not an error. Returns nil when absent or invalid.
func optionalString(data map[string]interface{}, name string, maxLen int, fields map[string]string) *string {
	v, present := data[name]
	if !present {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		fields[name] = "invalid"
		return nil
	}
	trimmed := strings.TrimSpace(s)
	if utf8.RuneCountInString(trimmed) > maxLen {
		fields[name] = "too_long"
		return nil
	}
	return &trimmed
}

// optionalUUID extracts an optional UUID-formatted string field (O-19
// hero.imageMediaId). It checks only the format — whether the id names a
// media item that exists and belongs to the shop is not part of this
// task's scope (docs/05-API.md's O-19 row lists no such rule, and
// checking it would pull in the media module).
func optionalUUID(data map[string]interface{}, name string, fields map[string]string) *openapi_types.UUID {
	v, present := data[name]
	if !present {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		fields[name] = "invalid"
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		fields[name] = "invalid"
		return nil
	}
	u := openapi_types.UUID(id)
	return &u
}

// optionalURL extracts an optional absolute http(s) URL field (O-19
// social.telegram/instagram), bounded by maxLen runes.
func optionalURL(data map[string]interface{}, name string, maxLen int, fields map[string]string) *string {
	v, present := data[name]
	if !present {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		fields[name] = "invalid"
		return nil
	}
	if utf8.RuneCountInString(s) > maxLen {
		fields[name] = "too_long"
		return nil
	}
	parsed, err := url.Parse(s)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		fields[name] = "invalid"
		return nil
	}
	return &s
}

// optionalHTTPSURL is optionalURL restricted to https (O-19
// contacts.mapUrl: "https only").
func optionalHTTPSURL(data map[string]interface{}, name string, maxLen int, fields map[string]string) *string {
	v, present := data[name]
	if !present {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		fields[name] = "invalid"
		return nil
	}
	if utf8.RuneCountInString(s) > maxLen {
		fields[name] = "too_long"
		return nil
	}
	parsed, err := url.Parse(s)
	if err != nil || parsed.Host == "" || parsed.Scheme != "https" {
		fields[name] = "invalid"
		return nil
	}
	return &s
}

func validateHero(data map[string]interface{}) (gen.ContentHero, map[string]string) {
	fields := map[string]string{}
	unknownFields(data, map[string]struct{}{"title": {}, "tagline": {}, "imageMediaId": {}}, fields)
	title := requiredString(data, "title", 200, fields)
	tagline := optionalString(data, "tagline", 300, fields)
	imageMediaID := optionalUUID(data, "imageMediaId", fields)
	if len(fields) > 0 {
		return gen.ContentHero{}, fields
	}
	return gen.ContentHero{Title: title, Tagline: tagline, ImageMediaId: imageMediaID}, nil
}

func validateAbout(data map[string]interface{}) (gen.ContentAbout, map[string]string) {
	fields := map[string]string{}
	unknownFields(data, map[string]struct{}{"title": {}, "body": {}}, fields)
	title := optionalString(data, "title", 200, fields)
	body := requiredString(data, "body", 5000, fields)
	if len(fields) > 0 {
		return gen.ContentAbout{}, fields
	}
	return gen.ContentAbout{Title: title, Body: body}, nil
}

func validateContacts(data map[string]interface{}) (gen.ContentContacts, map[string]string) {
	fields := map[string]string{}
	unknownFields(data, map[string]struct{}{"phone": {}, "address": {}, "mapUrl": {}}, fields)
	phone := requiredString(data, "phone", 30, fields)
	address := requiredString(data, "address", 300, fields)
	mapURL := optionalHTTPSURL(data, "mapUrl", 500, fields)
	if len(fields) > 0 {
		return gen.ContentContacts{}, fields
	}
	return gen.ContentContacts{Phone: phone, Address: address, MapUrl: mapURL}, nil
}

func validateSocial(data map[string]interface{}) (gen.ContentSocial, map[string]string) {
	fields := map[string]string{}
	unknownFields(data, map[string]struct{}{"telegram": {}, "instagram": {}}, fields)
	telegram := optionalURL(data, "telegram", 300, fields)
	instagram := optionalURL(data, "instagram", 300, fields)
	if len(fields) > 0 {
		return gen.ContentSocial{}, fields
	}
	return gen.ContentSocial{Telegram: telegram, Instagram: instagram}, nil
}

func validateSeo(data map[string]interface{}) (gen.ContentSeo, map[string]string) {
	fields := map[string]string{}
	unknownFields(data, map[string]struct{}{"title": {}, "description": {}}, fields)
	title := requiredString(data, "title", 70, fields)
	description := requiredString(data, "description", 200, fields)
	if len(fields) > 0 {
		return gen.ContentSeo{}, fields
	}
	return gen.ContentSeo{Title: title, Description: description}, nil
}

// hhmmPattern is the O-19 "HH:MM" shape, the same pattern
// contracts/openapi.yaml's ContentHoursDay.open/close carry.
var hhmmPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// requiredTime extracts a required "HH:MM" field.
func requiredTime(m map[string]interface{}, name string, fields map[string]string) *string {
	v, present := m[name]
	if !present {
		fields[name] = "required"
		return nil
	}
	s, ok := v.(string)
	if !ok || !hhmmPattern.MatchString(s) {
		fields[name] = "invalid"
		return nil
	}
	return &s
}

// optionalTime extracts an optional "HH:MM" field.
func optionalTime(m map[string]interface{}, name string, fields map[string]string) *string {
	v, present := m[name]
	if !present {
		return nil
	}
	s, ok := v.(string)
	if !ok || !hhmmPattern.MatchString(s) {
		fields[name] = "invalid"
		return nil
	}
	return &s
}

// validateHoursDay validates one element of ContentHours.days, returning
// its own field errors unprefixed — validateHours prefixes them with
// "days[<index>]." before merging into the outer fields map.
func validateHoursDay(item interface{}) (gen.ContentHoursDay, gen.ContentHoursDayDay, map[string]string) {
	fields := map[string]string{}
	m, ok := item.(map[string]interface{})
	if !ok {
		fields[""] = "invalid"
		return gen.ContentHoursDay{}, "", fields
	}
	unknownFields(m, map[string]struct{}{"day": {}, "closed": {}, "open": {}, "close": {}}, fields)

	var day gen.ContentHoursDayDay
	dayVal, dayPresent := m["day"]
	switch {
	case !dayPresent:
		fields["day"] = "required"
	default:
		s, ok := dayVal.(string)
		if !ok {
			fields["day"] = "invalid"
		} else if candidate := gen.ContentHoursDayDay(s); candidate.Valid() {
			day = candidate
		} else {
			fields["day"] = "invalid"
		}
	}

	closedVal, closedPresent := m["closed"]
	var closed bool
	switch {
	case !closedPresent:
		fields["closed"] = "required"
	default:
		b, ok := closedVal.(bool)
		if !ok {
			fields["closed"] = "invalid"
		} else {
			closed = b
		}
	}

	var openTime, closeTime *string
	if closed {
		openTime = optionalTime(m, "open", fields)
		closeTime = optionalTime(m, "close", fields)
	} else {
		openTime = requiredTime(m, "open", fields)
		closeTime = requiredTime(m, "close", fields)
		if openTime != nil && closeTime != nil && *openTime >= *closeTime {
			fields["close"] = "invalid"
		}
	}

	if len(fields) > 0 {
		return gen.ContentHoursDay{}, day, fields
	}
	return gen.ContentHoursDay{Day: day, Closed: closed, Open: openTime, Close: closeTime}, day, nil
}

func validateHours(data map[string]interface{}) (gen.ContentHours, map[string]string) {
	fields := map[string]string{}
	unknownFields(data, map[string]struct{}{"days": {}, "note": {}}, fields)
	note := optionalString(data, "note", 300, fields)

	rawDays, present := data["days"]
	if !present {
		fields["days"] = "required"
		return gen.ContentHours{}, fields
	}
	arr, ok := rawDays.([]interface{})
	if !ok || len(arr) != 7 {
		fields["days"] = "invalid"
		return gen.ContentHours{}, fields
	}

	seen := map[gen.ContentHoursDayDay]bool{}
	days := make([]gen.ContentHoursDay, len(arr))
	for i, item := range arr {
		day, key, dayFields := validateHoursDay(item)
		prefix := "days[" + strconv.Itoa(i) + "]"
		for k, v := range dayFields {
			if k == "" {
				fields[prefix] = v
				continue
			}
			fields[prefix+"."+k] = v
		}
		if key != "" {
			if seen[key] {
				fields[prefix+".day"] = "invalid"
			} else {
				seen[key] = true
			}
		}
		days[i] = day
	}
	// D-107 / O-19: "exactly 7 items", one per distinct mon..sun — the
	// length check above only guarantees the count, not that all 7 are
	// present without a repeat.
	if len(seen) != 7 {
		fields["days"] = "invalid"
	}

	if len(fields) > 0 {
		return gen.ContentHours{}, fields
	}
	return gen.ContentHours{Days: days, Note: note}, nil
}
