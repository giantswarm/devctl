package reposetup

// VisibilityPrivate and VisibilityPublic are the values of an entry's
// visibility, the schema's enum.
const (
	VisibilityPrivate = "private"
	VisibilityPublic  = "public"
)

// CreationVisibility is the visibility a repository is created with: the
// declared one, or private -- the org's default -- when the entry declares
// none. It is for a creation alone: for a repository that exists, an entry
// without a visibility leaves GitHub's as it is.
func CreationVisibility(declared string) string {
	if declared == "" {
		return VisibilityPrivate
	}
	return declared
}
