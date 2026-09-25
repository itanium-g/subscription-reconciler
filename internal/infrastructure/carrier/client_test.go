package carrier

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/subscription-reconciler/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestGetPlanStatusQueryEscapesUserID(t *testing.T) {
	const userID = "user/with +&? characters"
	observed := make(chan [2]string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- [2]string{r.URL.Path, r.URL.Query().Get("userId")}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"active"}`))
	}))
	defer server.Close()

	status, err := NewHTTPClient(server.URL).GetPlanStatus(context.Background(), userID)

	require.NoError(t, err)
	require.Equal(t, domain.CarrierStatusActive, status)
	request := <-observed
	require.Equal(t, "/mock/carrier/plan", request[0])
	require.Equal(t, userID, request[1])
}

func TestGetPlanStatusHonorsContextCancellation(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := NewHTTPClient(server.URL).GetPlanStatus(ctx, "user_1")
		result <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("carrier request did not start")
	}
	cancel()

	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("carrier request did not stop after context cancellation")
	}
}

func TestGetPlanStatusRejectsInvalidBaseURL(t *testing.T) {
	status, err := NewHTTPClient("://invalid").GetPlanStatus(context.Background(), "user_1")

	require.Equal(t, domain.CarrierStatusAPIError, status)
	require.Error(t, err)
}
