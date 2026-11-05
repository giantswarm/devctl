package circleciapp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

func TestEnsureSelectsTheRequestedCheckout(t *testing.T) {
	for _, tc := range []struct {
		name            string
		otherTriggered  bool
		matchingPresent bool
		matchingActive  bool
	}{
		{name: "unrelated dormant definition"},
		{name: "unrelated active definition", otherTriggered: true},
		{name: "matching dormant behind unrelated active", otherTriggered: true, matchingPresent: true},
		{name: "matching active behind unrelated active", otherTriggered: true, matchingPresent: true, matchingActive: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := circleciclient.AppDefinition("other-app", "", "7")
			other.ID = "other"
			ci := newFake(oauthDef(), other)
			ci.triggers["oauth"] = []circleciclient.Trigger{{ID: "implicit"}}
			if tc.otherTriggered {
				trigger := circleciclient.AllPushesTrigger("7")
				trigger.ID = "other-pushes"
				ci.triggers["other"] = []circleciclient.Trigger{trigger}
			}
			otherTriggers := append([]circleciclient.Trigger(nil), ci.triggers["other"]...)
			if tc.matchingPresent {
				ci.defs = append(ci.defs, appDef())
			}
			if tc.matchingActive {
				trigger := circleciclient.AllPushesTrigger("42")
				trigger.ID = "app-pushes"
				ci.triggers["app"] = []circleciclient.Trigger{trigger}
			}

			result, err := Ensure(context.Background(), ci, repo, true)
			require.NoError(t, err)
			require.Equal(t, "42", result.Definition.CheckoutSource.Repo.ExternalID,
				"migration must leave an App pipeline that builds the requested repository")
			wantID := "created"
			if tc.matchingPresent {
				wantID = "app"
			}
			require.Equal(t, wantID, result.Definition.ID)
			require.Equal(t, !tc.matchingPresent, result.CreatedDefinition)
			require.Equal(t, !tc.matchingActive, result.CreatedTrigger)
			require.Equal(t, otherTriggers, ci.triggers["other"], "unrelated triggers must be preserved")
			require.Equal(t, []string{"implicit"}, result.DeletedOAuthTriggers)
			require.Empty(t, ci.triggers["oauth"])
			require.Len(t, ci.triggers[wantID], 1)
			require.Equal(t, "42", ci.triggers[wantID][0].EventSource.Repo.ExternalID)

			ci.writes = nil
			again, err := Ensure(context.Background(), ci, repo, true)
			require.NoError(t, err)
			require.Equal(t, wantID, again.Definition.ID)
			require.False(t, again.Changed())
			require.Empty(t, ci.writes)
		})
	}
}
