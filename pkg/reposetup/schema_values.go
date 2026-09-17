package reposetup

import "encoding/json"

// EmbeddedFieldValues returns the values the embedded schema allows for a
// top-level entry field with an enum (componentType, visibility,
// lifecycle), so a command's help lists what the schema says instead of a
// copy that drifts. Nil for a field without an enum.
func EmbeddedFieldValues(field string) []string {
	var schema struct {
		Items struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"items"`
	}
	if err := json.Unmarshal(embeddedSchema, &schema); err != nil {
		return nil
	}
	return schema.Items.Properties[field].Enum
}
