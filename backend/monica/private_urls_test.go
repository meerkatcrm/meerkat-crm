package monica

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrivateURLSwitchOnMonicaClient(t *testing.T) {
	DisableRateLimitForTesting()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	blocked, err := NewClient(srv.URL, "tok", true)
	require.NoError(t, err)
	assert.ErrorIs(t, blocked.TestConnection(context.Background()), ErrPrivateAddress)
	assert.Zero(t, hits.Load())

	open, err := NewClient(srv.URL, "tok", false)
	require.NoError(t, err)
	_ = open.TestConnection(context.Background())
	assert.NotZero(t, hits.Load())
}
