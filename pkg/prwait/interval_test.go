package prwait

import (
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

func Test_interval(t *testing.T) {
	now := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	hour := now.Add(time.Hour)

	testCases := []struct {
		name string
		rate githubclient.RateLimit
		want time.Duration
	}{
		{name: "unknown budget polls at the floor", rate: githubclient.RateLimit{}, want: MinInterval},
		{name: "a full budget polls at the floor", rate: githubclient.RateLimit{Known: true, Remaining: 4999, Reset: hour}, want: MinInterval},
		{name: "a thin budget stretches: 200 left for an hour at 4 per poll is 72 s, capped", rate: githubclient.RateLimit{Known: true, Remaining: 200, Reset: hour}, want: MaxInterval},
		{name: "in between it is the budget spread over the window", rate: githubclient.RateLimit{Known: true, Remaining: 480, Reset: hour}, want: 30 * time.Second},
		{name: "an exhausted budget polls at the ceiling", rate: githubclient.RateLimit{Known: true, Remaining: 0, Reset: hour}, want: MaxInterval},
		{name: "a window already over polls at the floor", rate: githubclient.RateLimit{Known: true, Remaining: 10, Reset: now.Add(-time.Second)}, want: MinInterval},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := interval(tc.rate, now); got != tc.want {
				t.Errorf("want %s, got %s", tc.want, got)
			}
		})
	}
}
