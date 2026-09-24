package reposetup

import (
	"strings"
	"sync"

	"github.com/giantswarm/microerror"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// FieldValues returns the values the schema allows for a declaration field,
// in the schema's order. The path is the field's dotted name within an
// entry: componentType, gen.flavours, gen.language, visibility, lifecycle.
// For an array field (gen.flavours) it is the enum of the array's items.
//
// The values are read from the document the schema was compiled from,
// following its $refs, so the embedded schema and one fetched from
// giantswarm/github each answer from themselves.
//
// An unknown path, a field without an enum and an enum with a value that is
// not a string are errors, never an empty list.
func (s *Schema) FieldValues(path string) ([]string, error) {
	node := itemSchema(s.compiled)
	if node == nil {
		return nil, microerror.Maskf(invalidSchemaError, "the repositories schema declares no entry schema")
	}

	for _, name := range strings.Split(path, ".") {
		node = propertySchema(node, name)
		if node == nil {
			return nil, microerror.Maskf(invalidConfigError, "%q is not a field of the repositories schema", path)
		}
	}

	enum := enumOf(node)
	if enum == nil {
		enum = enumOf(itemSchema(node))
	}
	if enum == nil {
		return nil, microerror.Maskf(invalidConfigError, "field %q of the repositories schema has no enum", path)
	}

	values := make([]string, 0, len(enum.Values))
	for _, v := range enum.Values {
		value, ok := v.(string)
		if !ok {
			return nil, microerror.Maskf(invalidSchemaError, "field %q of the repositories schema allows %v, which is not a string", path, v)
		}
		values = append(values, value)
	}
	return values, nil
}

// itemSchema is the schema of an array's items, nil for a schema that
// declares none.
func itemSchema(s *jsonschema.Schema) *jsonschema.Schema {
	for ; s != nil; s = s.Ref {
		if s.Items2020 != nil {
			return s.Items2020
		}
		if items, ok := s.Items.(*jsonschema.Schema); ok {
			return items
		}
	}
	return nil
}

// propertySchema is the schema of an object's property, nil for a property
// the schema does not declare.
func propertySchema(s *jsonschema.Schema, name string) *jsonschema.Schema {
	for ; s != nil; s = s.Ref {
		if property, ok := s.Properties[name]; ok {
			return property
		}
	}
	return nil
}

// enumOf is the enum of a schema, nil for a schema without one.
func enumOf(s *jsonschema.Schema) *jsonschema.Enum {
	for ; s != nil; s = s.Ref {
		if s.Enum != nil {
			return s.Enum
		}
	}
	return nil
}

var embeddedSchemaOnce = sync.OnceValues(EmbeddedSchema)

// EmbeddedFieldValues returns the values the embedded schema allows for a
// declaration field (see [Schema.FieldValues]), so a command's help lists
// what the schema says instead of a copy that drifts. Nil for a field
// without an enum.
func EmbeddedFieldValues(field string) []string {
	schema, err := embeddedSchemaOnce()
	if err != nil {
		return nil
	}
	values, err := schema.FieldValues(field)
	if err != nil {
		return nil
	}
	return values
}
