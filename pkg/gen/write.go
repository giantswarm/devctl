package gen

import (
	"crypto/rand"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
)

// writeGeneratedFile publishes a complete sibling file after both writing and
// closing it succeed. A failed write leaves any previous output in place.
func writeGeneratedFile(destination string, content []byte, permissions fs.FileMode) error {
	var err error
	destination, err = resolveDestination(destination)
	if err != nil {
		return microerror.Mask(err)
	}

	var existing fs.FileInfo
	if info, err := os.Stat(destination); err == nil {
		existing = info
		if !info.Mode().IsRegular() {
			return microerror.Maskf(filePathError, "file %q is not a regular file", destination)
		}
		permissions = info.Mode().Perm()
		// Replacing a directory entry must not bypass a read-only output file.
		probe, err := os.OpenFile(destination, os.O_WRONLY, 0) // #nosec G304 -- same caller-selected destination as the generator output
		if err != nil {
			return microerror.Mask(err)
		}
		if err := probe.Close(); err != nil {
			return microerror.Mask(err)
		}
	} else if !os.IsNotExist(err) {
		return microerror.Mask(err)
	}

	// Unlike chmod after CreateTemp, opening with the requested creation mode
	// preserves the caller's umask for new outputs. O_EXCL prevents overwriting
	// any other entry if the random name is already occupied.
	tmpPath := filepath.Join(filepath.Dir(destination), ".devctl-gen-"+rand.Text())
	w, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, permissions) // #nosec G304 -- random sibling of the caller-selected output; O_EXCL prevents replacing an entry
	if err != nil {
		return microerror.Mask(err)
	}
	defer func() {
		_ = w.Close()
		_ = os.Remove(tmpPath)
	}()

	if _, err := w.Write(content); err != nil {
		return microerror.Mask(err)
	}
	if existing != nil {
		// Existing output permissions are independent of the current umask or
		// the Input's creation permissions, just as with an in-place write.
		if err := w.Chmod(existing.Mode()); err != nil {
			return microerror.Mask(err)
		}
	}
	if err := w.Sync(); err != nil {
		return microerror.Mask(err)
	}
	if err := w.Close(); err != nil {
		return microerror.Mask(err)
	}
	if err := os.Rename(tmpPath, destination); err != nil {
		return microerror.Mask(err)
	}
	return nil
}

// resolveDestination follows final symlinks without replacing the links. Resolve
// each parent before joining the leaf: cleaning a target such as link/../file
// before following link would select the wrong directory.
func resolveDestination(destination string) (string, error) {
	for links := 0; links < 255; links++ {
		parent, name := filepath.Split(destination)
		if parent == "" {
			parent = "."
		}
		parent, err := filepath.EvalSymlinks(parent)
		if err != nil {
			return "", microerror.Mask(err)
		}
		destination = filepath.Join(parent, name)
		info, err := os.Lstat(destination)
		if os.IsNotExist(err) {
			return destination, nil
		}
		if err != nil {
			return "", microerror.Mask(err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return destination, nil
		}
		target, err := os.Readlink(destination)
		if err != nil {
			return "", microerror.Mask(err)
		}
		switch {
		case filepath.IsAbs(target) || filepath.VolumeName(target) != "":
			destination = target
		case filepath.IsAbs(filepath.VolumeName(destination) + target):
			// A Windows target rooted on the link's drive, such as \file.
			destination = filepath.VolumeName(destination) + target
		default:
			destination = parent + string(os.PathSeparator) + target
		}
	}
	return "", microerror.Maskf(filePathError, "too many symlinks in %q", destination)
}
