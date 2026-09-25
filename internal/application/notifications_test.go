package application

import (
	"context"
	"errors"
	"testing"

	"github.com/example/subscription-reconciler/internal/infrastructure/postgres"
	"github.com/stretchr/testify/require"
)

type notificationDatabase struct {
	postgres.Database
	claimed   []postgres.Notification
	claimErr  error
	batchSize []int32
}

func (d *notificationDatabase) ClaimDueNotifications(_ context.Context, batchSize int32) ([]postgres.Notification, error) {
	d.batchSize = append(d.batchSize, batchSize)
	return d.claimed, d.claimErr
}

func TestSendDueNotificationsEmptySet(t *testing.T) {
	db := &notificationDatabase{}
	service := NewNotificationService(db)

	claimed, err := service.SendDueNotifications(context.Background(), 100)

	require.NoError(t, err)
	require.Zero(t, claimed)
	require.Equal(t, []int32{100}, db.batchSize)
}

func TestSendDueNotificationsReturnsClaimedCount(t *testing.T) {
	db := &notificationDatabase{
		claimed: []postgres.Notification{
			{ID: 1, UserID: "user_1"},
			{ID: 2, UserID: "user_2"},
		},
	}
	service := NewNotificationService(db)

	claimed, err := service.SendDueNotifications(context.Background(), 2)

	require.NoError(t, err)
	require.Equal(t, int32(2), claimed)
	require.Equal(t, []int32{2}, db.batchSize)
}

func TestSendDueNotificationsReturnsClaimError(t *testing.T) {
	wantErr := errors.New("claim notifications")
	db := &notificationDatabase{claimErr: wantErr}
	service := NewNotificationService(db)

	claimed, err := service.SendDueNotifications(context.Background(), 10)

	require.ErrorIs(t, err, wantErr)
	require.Zero(t, claimed)
	require.Equal(t, []int32{10}, db.batchSize)
}
