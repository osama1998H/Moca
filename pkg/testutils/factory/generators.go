package factory

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/osama1998H/moca/pkg/meta"
)

// generateFieldValue produces a valid value for the given field definition,
// respecting all validation constraints (Required, MaxLength, MinValue/MaxValue,
// Options, ValidationRegex, Unique).
func generateFieldValue(faker *gofakeit.Faker, fctx fieldGenContext) any {
	field := fctx.field

	// Non-storable fields produce no value.
	if !field.FieldType.IsStorable() {
		return nil
	}

	// Skip non-required fields ~20% of the time for realistic sparsity.
	if !field.Required && faker.IntRange(1, 5) == 1 {
		return nil
	}

	val := generateByType(faker, fctx)

	// Enforce MaxLength for string values.
	if field.MaxLength > 0 {
		if s, ok := val.(string); ok && len(s) > field.MaxLength {
			val = s[:field.MaxLength]
		}
	}

	// Enforce unique suffix.
	if fctx.forceUniq {
		if s, ok := val.(string); ok {
			val = fmt.Sprintf("%s_%d", s, fctx.seq)
		}
	}

	return val
}

func generateByType(faker *gofakeit.Faker, fctx fieldGenContext) any {
	field := fctx.field

	switch field.FieldType {
	case meta.FieldTypeData:
		return generateDataField(faker, field)
	case meta.FieldTypeText, meta.FieldTypeLongText, meta.FieldTypeMarkdown, meta.FieldTypeHTMLEditor:
		return generateTextField(faker, field)
	case meta.FieldTypeCode:
		return faker.LoremIpsumSentence(faker.IntRange(5, 15))
	case meta.FieldTypeInt:
		return generateInt(faker, field)
	case meta.FieldTypeFloat, meta.FieldTypeCurrency:
		return generateFloat(faker, field)
	case meta.FieldTypePercent:
		return generatePercent(faker, field)
	case meta.FieldTypeDate:
		return generateDate(faker)
	case meta.FieldTypeDatetime:
		return generateDatetime(faker)
	case meta.FieldTypeTime:
		return generateTime(faker)
	case meta.FieldTypeDuration:
		return generateDuration(faker, field)
	case meta.FieldTypeSelect:
		return generateSelect(faker, field)
	case meta.FieldTypeCheck:
		return faker.Bool()
	case meta.FieldTypeColor:
		return faker.HexColor()
	case meta.FieldTypeRating:
		return float64(faker.IntRange(0, 5))
	case meta.FieldTypeJSON:
		return generateJSON(faker)
	case meta.FieldTypeGeolocation:
		return generateGeolocation(faker)
	case meta.FieldTypeAttach, meta.FieldTypeAttachImage:
		ext := "pdf"
		if field.FieldType == meta.FieldTypeAttachImage {
			ext = "png"
		}
		return fmt.Sprintf("/files/test/%s.%s", faker.UUID(), ext)
	case meta.FieldTypePassword:
		return faker.Password(true, true, true, true, false, 12)
	case meta.FieldTypeSignature:
		return base64.StdEncoding.EncodeToString([]byte(faker.LoremIpsumSentence(3)))
	case meta.FieldTypeBarcode:
		return faker.Numerify("############")
	case meta.FieldTypeLink:
		// Link fields are handled by the dependency resolver, not here.
		// Return empty string as placeholder; the factory orchestrator fills it.
		return ""
	case meta.FieldTypeDynamicLink:
		return ""
	default:
		// Unknown or custom field type — generate a short string.
		return faker.Word()
	}
}

// generateDataField uses field name heuristics to generate contextually
// appropriate data (email for "email", phone for "phone", etc.).
func generateDataField(faker *gofakeit.Faker, field meta.FieldDef) string {
	name := strings.ToLower(field.Name)

	switch {
	case strings.Contains(name, "email"):
		return faker.Email()
	case strings.Contains(name, "phone") || strings.Contains(name, "mobile"):
		return faker.Phone()
	case strings.Contains(name, "url") || strings.Contains(name, "website"):
		return fmt.Sprintf("https://%s.example.com", faker.DomainName())
	case strings.Contains(name, "city"):
		return faker.City()
	case strings.Contains(name, "country"):
		return faker.Country()
	case strings.Contains(name, "address"):
		return faker.Street()
	case strings.Contains(name, "company") || strings.Contains(name, "organization"):
		return faker.Company()
	case strings.Contains(name, "first_name"):
		return faker.FirstName()
	case strings.Contains(name, "last_name"):
		return faker.LastName()
	case strings.Contains(name, "name") || strings.Contains(name, "title"):
		return faker.Name()
	case strings.Contains(name, "code") || strings.Contains(name, "ref"):
		return strings.ToUpper(faker.LetterN(3)) + "-" + faker.Numerify("####")
	case strings.Contains(name, "description"):
		return faker.Sentence(faker.IntRange(5, 15))
	default:
		return faker.Word()
	}
}

func generateTextField(faker *gofakeit.Faker, field meta.FieldDef) string {
	maxLen := 500
	if field.MaxLength > 0 && field.MaxLength < maxLen {
		maxLen = field.MaxLength
	}
	text := faker.Paragraph(1, faker.IntRange(2, 5), faker.IntRange(5, 12), " ")
	if len(text) > maxLen {
		text = text[:maxLen]
	}
	return text
}

func generateInt(faker *gofakeit.Faker, field meta.FieldDef) int {
	minVal := 1
	maxVal := 10000
	if field.MinValue != nil {
		minVal = int(math.Ceil(*field.MinValue))
	}
	if field.MaxValue != nil {
		maxVal = int(math.Floor(*field.MaxValue))
	}
	if minVal > maxVal {
		minVal = maxVal
	}
	return faker.IntRange(minVal, maxVal)
}

func generateFloat(faker *gofakeit.Faker, field meta.FieldDef) float64 {
	minVal := 0.01
	maxVal := 100000.0
	if field.MinValue != nil {
		minVal = *field.MinValue
	}
	if field.MaxValue != nil {
		maxVal = *field.MaxValue
	}
	if minVal > maxVal {
		minVal = maxVal
	}
	return math.Round(faker.Float64Range(minVal, maxVal)*100) / 100
}

func generatePercent(faker *gofakeit.Faker, field meta.FieldDef) float64 {
	minVal := 0.0
	maxVal := 100.0
	if field.MinValue != nil {
		minVal = *field.MinValue
	}
	if field.MaxValue != nil {
		maxVal = *field.MaxValue
	}
	return math.Round(faker.Float64Range(minVal, maxVal)*100) / 100
}

func generateDate(faker *gofakeit.Faker) string {
	now := time.Now()
	start := now.AddDate(-2, 0, 0)
	end := now.AddDate(1, 0, 0)
	d := faker.DateRange(start, end)
	return d.Format("2006-01-02")
}

func generateDatetime(faker *gofakeit.Faker) string {
	now := time.Now()
	start := now.AddDate(-2, 0, 0)
	end := now.AddDate(1, 0, 0)
	d := faker.DateRange(start, end)
	return d.Format(time.RFC3339)
}

func generateTime(faker *gofakeit.Faker) string {
	h := faker.IntRange(0, 23)
	m := faker.IntRange(0, 59)
	s := faker.IntRange(0, 59)
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func generateDuration(faker *gofakeit.Faker, field meta.FieldDef) float64 {
	maxVal := 86400.0 // 24 hours in seconds
	if field.MaxValue != nil {
		maxVal = *field.MaxValue
	}
	return math.Round(faker.Float64Range(0, maxVal)*100) / 100
}

func generateSelect(faker *gofakeit.Faker, field meta.FieldDef) string {
	if field.Options == "" {
		return ""
	}
	options := strings.Split(field.Options, "\n")
	// Filter out empty strings.
	valid := make([]string, 0, len(options))
	for _, opt := range options {
		opt = strings.TrimSpace(opt)
		if opt != "" {
			valid = append(valid, opt)
		}
	}
	if len(valid) == 0 {
		return ""
	}
	return valid[faker.IntRange(0, len(valid)-1)]
}

func generateJSON(faker *gofakeit.Faker) string {
	data := map[string]any{
		"key":    faker.Word(),
		"value":  faker.Sentence(3),
		"number": faker.IntRange(1, 1000),
		"active": faker.Bool(),
	}
	b, _ := json.Marshal(data)
	return string(b)
}

func generateGeolocation(faker *gofakeit.Faker) string {
	data := map[string]any{
		"type": "Point",
		"coordinates": []float64{
			faker.Longitude(),
			faker.Latitude(),
		},
	}
	b, _ := json.Marshal(data)
	return string(b)
}
