package reposetup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// tarEntry is one entry of a test tarball: a symlink when linkname is set,
// a regular file otherwise.
type tarEntry struct {
	name, content, linkname string
}

// buildTarball writes entries as a gzipped tarball, each name prefixed with
// the archive's stripped top-level directory the way GitHub's tarballs are,
// the same "owner-repo-sha/" extractTarball trims off.
func buildTarball(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		name := "top/" + e.name
		if e.linkname != "" {
			require.NoError(t, tw.WriteHeader(&tar.Header{
				Name:     name,
				Typeflag: tar.TypeSymlink,
				Linkname: e.linkname,
				Mode:     0o777,
			}))
			continue
		}
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Size:     int64(len(e.content)),
			Mode:     0o644,
		}))
		_, err := tw.Write([]byte(e.content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// TestExtractTarball_Symlinks: an in-tree symlink is recreated, an
// absolute-target symlink and one escaping the target directory via ".." are
// both refused -- skipped rather than followed or written outside dir, so a
// hostile template tarball cannot place a file outside the scaffold.
func TestExtractTarball_Symlinks(t *testing.T) {
	data := buildTarball(t, []tarEntry{
		{name: "AGENTS.md", content: "agents"},
		{name: "CLAUDE.md", linkname: "AGENTS.md"},
		{name: "escapes.md", linkname: "../../../../etc/passwd"},
		{name: "absolute.md", linkname: "/etc/passwd"},
	})

	dir := t.TempDir()
	require.NoError(t, extractTarball(bytes.NewReader(data), dir))

	// The in-tree symlink is kept, pointing at its literal target.
	target, err := os.Readlink(filepath.Join(dir, "CLAUDE.md"))
	require.NoError(t, err)
	require.Equal(t, "AGENTS.md", target)
	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	require.Equal(t, "agents", string(got))

	// The unsafe symlinks were not created at all.
	_, err = os.Lstat(filepath.Join(dir, "escapes.md"))
	require.True(t, os.IsNotExist(err), "an escaping symlink must not be written")
	_, err = os.Lstat(filepath.Join(dir, "absolute.md"))
	require.True(t, os.IsNotExist(err), "an absolute-target symlink must not be written")

	// Nothing was written outside dir.
	_, err = os.Lstat(filepath.Join(filepath.Dir(dir), "etc"))
	require.True(t, os.IsNotExist(err))
}

// TestCopyTree_Symlinks mirrors TestExtractTarball_Symlinks for the local
// checkout path DirTemplates/copyTree serves (tests, offline use).
func TestCopyTree_Symlinks(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "AGENTS.md"), []byte("agents"), 0o644))
	require.NoError(t, os.Symlink("AGENTS.md", filepath.Join(src, "CLAUDE.md")))
	require.NoError(t, os.Symlink("../../../../etc/passwd", filepath.Join(src, "escapes.md")))
	require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(src, "absolute.md")))
	require.NoError(t, os.MkdirAll(filepath.Join(src, ".agents", "skills", "x"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(src, ".claude", "skills"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join("..", "..", ".agents", "skills", "x"), filepath.Join(src, ".claude", "skills", "x")))

	dst := t.TempDir()
	require.NoError(t, copyTree(src, dst))

	target, err := os.Readlink(filepath.Join(dst, "CLAUDE.md"))
	require.NoError(t, err)
	require.Equal(t, "AGENTS.md", target)

	skillTarget, err := os.Readlink(filepath.Join(dst, ".claude", "skills", "x"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join("..", "..", ".agents", "skills", "x"), skillTarget)

	_, err = os.Lstat(filepath.Join(dst, "escapes.md"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Lstat(filepath.Join(dst, "absolute.md"))
	require.True(t, os.IsNotExist(err))
}
