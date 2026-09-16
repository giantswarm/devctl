package reposetup

import "github.com/giantswarm/microerror"

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == invalidConfigError
}

var invalidTeamFileError = &microerror.Error{
	Kind: "invalidTeamFileError",
}

// IsInvalidTeamFile asserts invalidTeamFileError: the team file is not a
// YAML list of mappings.
func IsInvalidTeamFile(err error) bool {
	return microerror.Cause(err) == invalidTeamFileError
}

var invalidSchemaError = &microerror.Error{
	Kind: "invalidSchemaError",
}

// IsInvalidSchema asserts invalidSchemaError: the repositories schema does
// not parse or compile.
func IsInvalidSchema(err error) bool {
	return microerror.Cause(err) == invalidSchemaError
}

var entryNotFoundError = &microerror.Error{
	Kind: "entryNotFoundError",
}

// IsEntryNotFound asserts entryNotFoundError: a requested entry name is not
// in the team file.
func IsEntryNotFound(err error) bool {
	return microerror.Cause(err) == entryNotFoundError
}

var templateUnavailableError = &microerror.Error{
	Kind: "templateUnavailableError",
}

// IsTemplateUnavailable asserts templateUnavailableError: the declaration
// needs a template that does not exist yet.
func IsTemplateUnavailable(err error) bool {
	return microerror.Cause(err) == templateUnavailableError
}

var templateFetchError = &microerror.Error{
	Kind: "templateFetchError",
}

// IsTemplateFetch asserts templateFetchError: a template repository could
// not be fetched.
func IsTemplateFetch(err error) bool {
	return microerror.Cause(err) == templateFetchError
}

var renderFailedError = &microerror.Error{
	Kind: "renderFailedError",
}

// IsRenderFailed asserts renderFailedError: a generator failed while the
// scaffold was rendered.
func IsRenderFailed(err error) bool {
	return microerror.Cause(err) == renderFailedError
}
