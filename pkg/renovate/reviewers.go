// Package renovate provides helpers to surgically edit Renovate configuration
// files (renovate.json and renovate.json5) in place, preserving comments,
// quoting style and key order of the original file.
package renovate

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/giantswarm/microerror"
)

// configFileNames lists the candidate Renovate config filenames in preference
// order. renovate.json5 is preferred over renovate.json because it is the
// Giant Swarm house style.
var configFileNames = []string{"renovate.json5", "renovate.json"}

// FindConfigFile returns the path to the Renovate config file found in dir,
// preferring renovate.json5 over renovate.json. It returns a configNotFoundError
// if neither file exists.
func FindConfigFile(dir string) (string, error) {
	for _, name := range configFileNames {
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err == nil && !info.IsDir() {
			return p, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", microerror.Mask(err)
		}
	}

	return "", microerror.Maskf(configNotFoundError, "no Renovate config (%s) found in %#q", strings.Join(configFileNames, " or "), dir)
}

// SetReviewers sets the top-level `reviewers` array in the Renovate config at
// path to the given reviewers and writes the result back in place. Everything
// else in the file (comments, quoting, key order, formatting) is preserved
// byte-for-byte. If the file already has a `reviewers` key, only its value is
// replaced; otherwise the key is inserted as the first member of the root
// object.
//
// The rendered value matches the quoting style the file already uses (single
// or double quotes, dominant wins for mixed files). The file extension is only
// a fallback for empty or quote-less files: renovate.json5 defaults to single
// quotes (the Giant Swarm house style), renovate.json to strict-JSON double
// quotes.
func SetReviewers(path string, reviewers []string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return microerror.Mask(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return microerror.Mask(err)
	}

	defaultSingle := strings.EqualFold(filepath.Ext(path), ".json5")

	out, err := setReviewers(src, reviewers, defaultSingle)
	if err != nil {
		return microerror.Mask(err)
	}

	err = writeFileAtomic(path, out, info.Mode())
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// writeFileAtomic writes data to path by writing to a temporary file in the
// same directory and renaming it into place. The rename is atomic, so an
// interrupted or failed write never leaves the original file truncated.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return microerror.Mask(err)
	}
	tmpName := tmp.Name()
	// Best effort: if we return before a successful rename, drop the temp file.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return microerror.Mask(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return microerror.Mask(err)
	}
	if err := tmp.Close(); err != nil {
		return microerror.Mask(err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// setReviewers performs the in-memory edit. It is separated from SetReviewers
// so it can be unit-tested without touching the filesystem. defaultSingle is
// the fallback quote style used only when the file itself reveals no
// preference (empty or quote-less).
func setReviewers(src []byte, reviewers []string, defaultSingle bool) ([]byte, error) {
	objStart, err := findRootObjectStart(src)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	span, found, err := findTopLevelKey(src, objStart, "reviewers")
	if err != nil {
		return nil, microerror.Mask(err)
	}

	style := analyzeRootStyle(src, objStart, defaultSingle)
	array := renderArray(reviewers, style.valueQuote)

	out := make([]byte, 0, len(src)+len(array)+16)

	if found {
		// Replace just the value span, leaving the key and everything else
		// untouched.
		out = append(out, src[:span.valueStart]...)
		out = append(out, array...)
		out = append(out, src[span.valueEnd:]...)
		return out, nil
	}

	// Insert `reviewers: [...]` as the first key of the root object, right
	// after the opening brace, matching the file's existing key-quoting style
	// (bare identifier vs quoted string) and member indentation.
	key := "reviewers"
	if style.keyQuoted {
		key = string(style.keyQuote) + "reviewers" + string(style.keyQuote)
	}

	// When the root object already has members, a trailing comma after our
	// entry is just the separator before the existing first key and is valid
	// everywhere. When the object is empty, that comma would be a dangling
	// trailing comma which strict JSON (renovate.json) rejects, so omit it.
	var insertion string
	if rootObjectIsEmpty(src, objStart) {
		insertion = "\n" + style.indent + key + ": " + array + "\n"
	} else {
		insertion = "\n" + style.indent + key + ": " + array + ","
	}

	out = append(out, src[:objStart+1]...)
	out = append(out, insertion...)
	out = append(out, src[objStart+1:]...)
	return out, nil
}

// rootObjectIsEmpty reports whether the object opening at objStart has no
// members (only whitespace and/or comments before its closing brace).
func rootObjectIsEmpty(src []byte, objStart int) bool {
	i := skipSpaceAndComments(src, objStart+1)
	return i < len(src) && src[i] == '}'
}

// renderArray renders the reviewers as a single-line JSON(5) array using the
// given quote character.
func renderArray(reviewers []string, quote byte) string {
	q := string(quote)

	items := make([]string, len(reviewers))
	for i, r := range reviewers {
		items[i] = q + escapeString(r, q) + q
	}

	return "[" + strings.Join(items, ", ") + "]"
}

// escapeString escapes the quote character and backslashes in s so the rendered
// string literal stays valid. Reviewer slugs do not normally contain these, but
// we guard against it to avoid producing a broken file.
func escapeString(s, quote string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, quote, `\`+quote)
	return s
}
