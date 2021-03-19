package gen

import (
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/pflag"
)

const (
	ArchitectureDarwin      Architecture = "darwin-amd64"
	ArchitectureLinux       Architecture = "linux-amd64"
	ArchitectureDarwinARM64 Architecture = "darwin-arm64"
	ArchitectureLinuxARM64  Architecture = "linux-arm64"
)

func AllArchitectures() []string {
	return []string{
		ArchitectureDarwin.String(),
		ArchitectureLinux.String(),
		ArchitectureDarwinARM64.String(),
		ArchitectureLinuxARM64.String(),
	}
}

type Architecture string

func NewArchitecture(s string) (Architecture, error) {
	switch s {
	case ArchitectureDarwin.String():
		return ArchitectureDarwin, nil
	case ArchitectureLinux.String():
		return ArchitectureLinux, nil
	case ArchitectureDarwinARM64.String():
		return ArchitectureDarwinARM64, nil
	case ArchitectureLinuxARM64.String():
		return ArchitectureLinuxARM64, nil
	}

	return Architecture("unknown"), microerror.Maskf(invalidConfigError, "Architecture must be one of %s", strings.Join(AllArchitectures(), "|"))
}

func (f Architecture) String() string {
	return string(f)
}

type ArchitectureSlice []Architecture

func (s ArchitectureSlice) Contains(f Architecture) bool {
	for _, x := range s {
		if x == f {
			return true
		}
	}
	return false
}

type ArchitectureSliceFlagValue struct {
	value   *ArchitectureSlice
	changed bool
}

var _ pflag.Value = new(ArchitectureSliceFlagValue)
var _ pflag.SliceValue = new(ArchitectureSliceFlagValue)

func NewArchitectureSliceFlagValue(p *ArchitectureSlice, value ArchitectureSlice) *ArchitectureSliceFlagValue {
	*p = value
	return &ArchitectureSliceFlagValue{
		value: p,
	}
}

func (s *ArchitectureSliceFlagValue) Set(val string) error {
	ss := strings.Split(val, ",")
	out := make([]Architecture, len(ss))
	for i, v := range ss {
		var err error
		out[i], err = NewArchitecture(v)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	if !s.changed {
		*s.value = out
	} else {
		*s.value = append(*s.value, out...)
	}

	s.changed = true
	return nil
}

func (s *ArchitectureSliceFlagValue) Type() string {
	return "ArchitectureSlice"
}

func (s *ArchitectureSliceFlagValue) String() string {
	out := make([]string, len(*s.value))
	for i, x := range *s.value {
		out[i] = x.String()
	}
	return "[" + strings.Join(out, ",") + "]"
}

func (s *ArchitectureSliceFlagValue) Append(val string) error {
	x, err := NewArchitecture(val)
	if err != nil {
		return microerror.Mask(err)
	}

	*s.value = append(*s.value, x)
	return nil
}

func (s *ArchitectureSliceFlagValue) Replace(val []string) error {
	out := make([]Architecture, len(val))
	for i, x := range val {
		var err error
		out[i], err = NewArchitecture(x)
		if err != nil {
			return microerror.Mask(err)
		}
	}
	*s.value = out
	return nil
}

func (s *ArchitectureSliceFlagValue) GetSlice() []string {
	out := make([]string, len(*s.value))
	for i, x := range *s.value {
		out[i] = x.String()
	}
	return out
}

func (s *ArchitectureSliceFlagValue) Default() []string { return nil }
