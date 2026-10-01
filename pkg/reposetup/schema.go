package reposetup

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// Where the live schema lives: giantswarm/github's main branch.
const (
	SchemaRepositoryOwner = "giantswarm"
	SchemaRepository      = "github"
	SchemaPath            = ".github/repositories.schema.json"
	SchemaRef             = "main"
)

// SchemaOrigin says where the schema a result was validated against came from.
type SchemaOrigin string

const (
	// SchemaOriginGitHub is the schema as fetched from giantswarm/github main.
	SchemaOriginGitHub SchemaOrigin = "giantswarm/github@main"
	// SchemaOriginEmbedded is the copy shipped with this devctl version.
	SchemaOriginEmbedded SchemaOrigin = "embedded"
	// SchemaOriginFile is a schema read from a local file.
	SchemaOriginFile SchemaOrigin = "file"
)

//go:embed schema/repositories.schema.json
var embeddedSchema []byte

// Schema is a compiled repositories schema: the JSON schema of the team
// files, whose top level is the list of entries.
type Schema struct {
	Origin   SchemaOrigin
	compiled *jsonschema.Schema
}

// FileGetter reads one file of a GitHub repository at a ref.
// *githubclient.Client is a FileGetter.
type FileGetter interface {
	GetFile(ctx context.Context, owner, repo, path, ref string) (githubclient.RepositoryFile, error)
}

// EmbeddedSchema compiles the schema copy shipped with this devctl version.
// It carries the fields of the repository set-up plan (description,
// visibility, lifecycle archived) and is the fallback when GitHub cannot be
// reached.
func EmbeddedSchema() (*Schema, error) {
	return CompileSchema(embeddedSchema, SchemaOriginEmbedded)
}

// FetchSchema reads and compiles the schema from giantswarm/github main, so
// a validation follows the schema as it is today.
func FetchSchema(ctx context.Context, files FileGetter) (*Schema, error) {
	if files == nil {
		return nil, microerror.Maskf(invalidConfigError, "FileGetter must not be nil")
	}

	file, err := files.GetFile(ctx, SchemaRepositoryOwner, SchemaRepository, SchemaPath, SchemaRef)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return CompileSchema(file.Data, SchemaOriginGitHub)
}

// CompileSchema compiles a repositories schema document.
func CompileSchema(doc []byte, origin SchemaOrigin) (*Schema, error) {
	parsed, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return nil, microerror.Maskf(invalidSchemaError, "parsing the repositories schema: %v", err)
	}

	const url = "repositories.schema.json"
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(url, parsed); err != nil {
		return nil, microerror.Maskf(invalidSchemaError, "loading the repositories schema: %v", err)
	}
	compiled, err := compiler.Compile(url)
	if err != nil {
		return nil, microerror.Maskf(invalidSchemaError, "compiling the repositories schema: %v", err)
	}

	return &Schema{Origin: origin, compiled: compiled}, nil
}

// Problems validates one entry — a JSON-compatible object, see
// [Declaration.Instance] — against the schema and returns a problem per
// violation, each naming the field.
func (s *Schema) Problems(entry any) []Problem {
	err := s.compiled.Validate([]any{entry})
	if err == nil {
		return nil
	}

	verr, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return []Problem{{Field: entryField, Message: err.Error()}}
	}

	var problems []Problem
	for _, leaf := range leafErrors(verr) {
		problems = append(problems, leafProblems(leaf)...)
	}
	return problems
}

// entryField names the entry as a whole where a problem has no field.
const entryField = "(entry)"

var englishPrinter = message.NewPrinter(language.English)

// leafErrors flattens the validation error tree to the errors that carry a
// keyword violation; the inner nodes only group them.
func leafErrors(err *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(err.Causes) == 0 {
		return []*jsonschema.ValidationError{err}
	}
	var leaves []*jsonschema.ValidationError
	for _, cause := range err.Causes {
		leaves = append(leaves, leafErrors(cause)...)
	}
	return leaves
}

// leafProblems names the field of one violation. The instance location
// starts with the entry's index in the one-element list Problems validates,
// which is dropped; a missing or unknown property is named itself.
func leafProblems(err *jsonschema.ValidationError) []Problem {
	location := err.InstanceLocation
	if len(location) > 0 {
		location = location[1:]
	}

	switch k := err.ErrorKind.(type) {
	case *kind.Required:
		problems := make([]Problem, 0, len(k.Missing))
		for _, missing := range k.Missing {
			problems = append(problems, Problem{Field: fieldName(append(location, missing)), Message: "required"})
		}
		return problems
	case *kind.AdditionalProperties:
		problems := make([]Problem, 0, len(k.Properties))
		for _, property := range k.Properties {
			problems = append(problems, Problem{Field: fieldName(append(location, property)), Message: "not a field of the repositories schema"})
		}
		return problems
	}

	return []Problem{{Field: fieldName(location), Message: err.ErrorKind.LocalizedString(englishPrinter)}}
}

// fieldName renders an instance location in dotted form: gen.ci.chartName,
// gen.flavours[1].
func fieldName(location []string) string {
	if len(location) == 0 {
		return entryField
	}
	var b strings.Builder
	for i, token := range location {
		if _, err := strconv.Atoi(token); err == nil {
			fmt.Fprintf(&b, "[%s]", token)
			continue
		}
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(token)
	}
	return b.String()
}
