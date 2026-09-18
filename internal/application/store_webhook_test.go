package application

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestComputeStateTransitionExpiredGrant(t *testing.T) {
	eventTimeMs := time.Now().AddDate(0, -2, 0).UnixMilli()

	active, expiresAt, reason := (&storeWebhookService{}).computeStateTransition("INITIAL_PURCHASE", eventTimeMs)

	require.False(t, active)
	require.NotNil(t, expiresAt)
	require.True(t, expiresAt.Before(time.Now()))
	require.Equal(t, "EXPIRATION", reason)
}
