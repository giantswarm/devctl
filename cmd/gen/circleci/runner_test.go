package circleci

import (
	"os"
	"path/filepath"
	"testing"
)

// Test_detectNodeVersion covers the .nvmrc probe that lets a repo own its Node
// version in one place. The empty results matter as much as the parsed ones:
// every repo predating this probe has no .nvmrc, and .nvmrc also accepts forms
// (aliases, partial versions) that name no cimg/node tag. Both must fall back
// to devctl's baked-in default rather than render an image that cannot be
// pulled.
func Test_detectNodeVersion(t *testing.T) {
	testCases := []struct {
		name string
		// nvmrc is the file content; absent means no .nvmrc is written at all.
		nvmrc  string
		absent bool
		want   string
	}{
		{
			name:   "no .nvmrc keeps the baked-in default",
			absent: true,
			want:   "",
		},
		{
			name:  "exact version",
			nvmrc: "24.19.0\n",
			want:  "24.19.0",
		},
		{
			name:  "leading v is stripped",
			nvmrc: "v24.19.0\n",
			want:  "24.19.0",
		},
		{
			name:  "no trailing newline",
			nvmrc: "24.19.0",
			want:  "24.19.0",
		},
		{
			name:  "surrounding whitespace is trimmed",
			nvmrc: "  24.19.0  \n",
			want:  "24.19.0",
		},
		{
			name:  "trailing comment is stripped",
			nvmrc: "24.19.0 # keep in sync with the backend Dockerfile\n",
			want:  "24.19.0",
		},
		{
			name:  "leading comment lines are skipped",
			nvmrc: "# the one source of truth\n24.19.0\n",
			want:  "24.19.0",
		},
		{
			name:  "empty file",
			nvmrc: "",
			want:  "",
		},
		{
			name:  "whitespace-only file",
			nvmrc: "\n\n  \n",
			want:  "",
		},
		{
			name:  "lts alias names no cimg/node tag",
			nvmrc: "lts/*\n",
			want:  "",
		},
		{
			name:  "node alias names no cimg/node tag",
			nvmrc: "node\n",
			want:  "",
		},
		{
			name:  "major-only version names no cimg/node tag",
			nvmrc: "24\n",
			want:  "",
		},
		{
			name:  "major.minor version names no cimg/node tag",
			nvmrc: "24.19\n",
			want:  "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// detectNodeVersion reads from the working directory, the same as
			// the Dockerfile and lockfile probes beside it.
			dir := t.TempDir()
			if !tc.absent {
				path := filepath.Join(dir, ".nvmrc")
				if err := os.WriteFile(path, []byte(tc.nvmrc), 0o600); err != nil {
					t.Fatalf("write .nvmrc: %v", err)
				}
			}
			t.Chdir(dir)

			got := detectNodeVersion()
			if got != tc.want {
				t.Errorf("detectNodeVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}
