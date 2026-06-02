package worker

import "context"

// Jobs represents background job implementations.
type Jobs struct {
	// TODO: Inject services and repositories
}

// NewJobs creates a new job runner.
func NewJobs() *Jobs {
	return &Jobs{}
}

// CarrierPollingJob polls the carrier API for all users.
func (j *Jobs) CarrierPollingJob(ctx context.Context) error {
	// TODO: Implement carrier polling
	// - Query users with source=CARRIER
	// - Use FOR UPDATE SKIP LOCKED
	// - Call mock carrier API
	// - Update carrier state
	return nil
}

// NotificationSenderJob sends scheduled notifications.
func (j *Jobs) NotificationSenderJob(ctx context.Context) error {
	// TODO: Implement notification sending
	// - Query notifications due for sending
	// - Send (in this case, update sent_at)
	// - Handle errors gracefully
	return nil
}
