package ir

import (
	"testing"
)

func TestValidatorIRRoundTrip(t *testing.T) {
	assertJSONRoundTrip(t, ValidatorIR{
		Type: "stringvalidator.OneOf",
		Args: []string{"pending", "running", "done"},
	})

	assertJSONRoundTrip(t, ValidatorIR{
		Type: "int64validator.Between",
		Args: []string{"0", "100"},
	})

	assertJSONRoundTrip(t, ValidatorIR{
		Type: "stringvalidator.RegexMatches",
		Args: []string{"^[0-9a-f-]+$", "must be a UUID"},
	})

	assertJSONRoundTrip(t, ValidatorIR{
		Type: "float64validator.AtLeast",
		Args: []string{"0.5"},
	})

	assertJSONRoundTrip(t, ValidatorIR{
		Type: "",
		Args: nil,
	})
}
