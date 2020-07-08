package gen

import (
	"strings"

	"github.com/giantswarm/microerror"
)

const (
	Daily   Schedule = "daily"
	Weekly  Schedule = "weekly"
	Monthly Schedule = "monthly"
)

type Schedule string

func NewSchedule(s string) (Schedule, error) {
	switch s {
	case Daily.String():
		return Daily, nil
	case Weekly.String():
		return Weekly, nil
	case Monthly.String():
		return Monthly, nil
	}

	return Schedule("unknown"), microerror.Maskf(invalidConfigError, "schedule must be one of %s", strings.Join(AllowedSchedule(), "|"))
}

func (s Schedule) String() string {
	return string(s)
}

func AllowedSchedule() []string {
	return []string{
		Daily.String(),
		Weekly.String(),
		Monthly.String(),
	}
}

func IsValidSchedule(s string) bool {
	_, err := NewSchedule(s)
	return err == nil
}
