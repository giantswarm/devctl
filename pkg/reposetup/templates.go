package reposetup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/giantswarm/microerror"
)

// TemplateSource fetches template repositories.
type TemplateSource interface {
	// Fetch writes the tree of the template repository (owner/name) into
	// dir, which exists and is empty.
	Fetch(ctx context.Context, repository, dir string) error
}

// GitHubTemplates fetches the tarball of a template's branch from GitHub:
// what the scaffold is rendered from at run time.
type GitHubTemplates struct {
	// Ref is the branch or tag; default main.
	Ref string
	// Token authenticates the download. giantswarm/template is private, so
	// the Go template needs it (GitHub answers 404 without); for the public
	// templates it raises the rate limit.
	Token string
	// Client is the HTTP client; default [http.DefaultClient].
	Client *http.Client
}

// DefaultTemplateRef is the branch templates are rendered from.
const DefaultTemplateRef = "main"

// Fetch downloads and extracts the tarball of the repository's Ref.
func (g GitHubTemplates) Fetch(ctx context.Context, repository, dir string) error {
	ref := g.Ref
	if ref == "" {
		ref = DefaultTemplateRef
	}
	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/tarball/%s", repository, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return microerror.Mask(err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return microerror.Maskf(templateFetchError, "fetching %s@%s: %v", repository, ref, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return microerror.Maskf(templateFetchError, "fetching %s@%s: HTTP %d", repository, ref, resp.StatusCode)
	}

	if err := extractTarball(resp.Body, dir); err != nil {
		return microerror.Maskf(templateFetchError, "extracting %s@%s: %v", repository, ref, err)
	}

	return nil
}

// DirTemplates serves templates from local checkouts under Root, one
// directory per template named after the repository without its owner
// (<Root>/template, <Root>/template-app). Tests and offline use.
type DirTemplates struct {
	Root string
}

// Fetch copies the checkout into dir, without its .git directory.
func (d DirTemplates) Fetch(_ context.Context, repository, dir string) error {
	src := filepath.Join(d.Root, path.Base(repository))
	if _, err := os.Stat(src); err != nil {
		return microerror.Maskf(templateFetchError, "no checkout of %s at %s: %v", repository, src, err)
	}
	return copyTree(src, dir)
}

// extractTarball extracts a GitHub tarball into dir: the archive's single
// top-level directory is stripped, regular files keep their executable bit,
// a symlink is recreated when its target is safe (see safeSymlinkTarget),
// and everything else (an unsafe symlink, the pax headers, other entry
// types) is skipped.
func extractTarball(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return microerror.Mask(err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return microerror.Mask(err)
		}

		_, rel, found := strings.Cut(strings.TrimPrefix(hdr.Name, "./"), "/")
		if !found || rel == "" {
			continue
		}
		target, err := safeJoin(dir, rel)
		if err != nil {
			return microerror.Mask(err)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, dirMode); err != nil {
				return microerror.Mask(err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
				return microerror.Mask(err)
			}
			mode := fileMode
			if hdr.FileInfo().Mode()&0o111 != 0 {
				mode = executableMode
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode) // #nosec G304 -- target is confined to dir by safeJoin
			if err != nil {
				return microerror.Mask(err)
			}
			_, err = io.Copy(f, tr) // #nosec G110 -- a template repository's tarball, a few hundred kilobytes; the caller chose the repository
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return microerror.Mask(err)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
				return microerror.Mask(err)
			}
			if !safeSymlinkTarget(dir, target, hdr.Linkname) {
				// An absolute target, or one that escapes dir once resolved
				// from the link's own directory: skipped like any other
				// entry this function does not support, rather than
				// failing the whole extraction over one untrusted link.
				continue
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return microerror.Mask(err)
			}
		}
	}
}

// safeSymlinkTarget says whether linkname is safe to create a symlink at
// target with: not an absolute path, and its resolution from target's own
// directory does not escape dir. target is already confined to dir (by
// safeJoin, or by dst being copyTree's own root) -- this is that same
// confinement extended to what a symlink may point at, since safeJoin only
// confines the link itself, never where it leads.
func safeSymlinkTarget(dir, target, linkname string) bool {
	if linkname == "" || filepath.IsAbs(filepath.FromSlash(linkname)) {
		return false
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(target), filepath.FromSlash(linkname)))
	return resolved == dir || strings.HasPrefix(resolved, dir+string(filepath.Separator))
}

// copyTree copies src into dst, skipping .git, keeping the executable bit
// and recreating a symlink (with the same confinement extractTarball uses,
// see safeSymlinkTarget) rather than following it.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return microerror.Mask(err)
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return microerror.Mask(err)
		}
		if d.IsDir() {
			if d.Name() == gitDir && rel != "." {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), dirMode)
		}
		target := filepath.Join(dst, rel)
		if d.Type()&fs.ModeSymlink != 0 {
			linkname, err := os.Readlink(p) // #nosec G304 -- p is the caller's own template checkout, walked by WalkDir
			if err != nil {
				return microerror.Mask(err)
			}
			if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
				return microerror.Mask(err)
			}
			if !safeSymlinkTarget(dst, target, linkname) {
				// Same refusal as extractTarball's: skip rather than copy
				// a link that would point outside dst once recreated there.
				return nil
			}
			return os.Symlink(linkname, target) // #nosec G122 -- target is under dst, the scaffold directory this package creates; linkname passed safeSymlinkTarget
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return microerror.Mask(err)
		}
		data, err := os.ReadFile(p) // #nosec G304 G122 -- walking the caller's own template checkout; a symlink swapped in during the walk is the caller's
		if err != nil {
			return microerror.Mask(err)
		}
		mode := fileMode
		if info.Mode()&0o111 != 0 {
			mode = executableMode
		}
		return writeFile(target, data, mode)
	})
}

// safeJoin joins rel onto dir, refusing a path that escapes dir.
func safeJoin(dir, rel string) (string, error) {
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if target != dir && !strings.HasPrefix(target, dir+string(filepath.Separator)) {
		return "", microerror.Maskf(invalidConfigError, "archive entry %q escapes the target directory", rel)
	}
	return target, nil
}

// gitDir is the directory a checkout keeps its history in; never part of
// a scaffold.
const gitDir = ".git"

const (
	dirMode        fs.FileMode = 0o750
	fileMode       fs.FileMode = 0o644
	executableMode fs.FileMode = 0o755
)

// writeFile writes data to p, creating parent directories.
func writeFile(p string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), dirMode); err != nil {
		return microerror.Mask(err)
	}
	if err := os.WriteFile(p, data, mode); err != nil { // #nosec G306 G703 -- p is a path under the scaffold directory this package created (safeJoin confines archive entries); 0644 is the files' mode in the repository
		return microerror.Mask(err)
	}
	return nil
}
