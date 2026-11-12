package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBlockedIP(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1": true, "10.1.2.3": true, "172.16.0.1": true, "192.168.1.1": true,
		"169.254.169.254": true, "0.0.0.0": true, "::": true, "::1": true, "fe80::1": true,
		"fd00::1": true, "::ffff:127.0.0.1": true, "::ffff:10.0.0.1": true, "::ffff:169.254.169.254": true,
		"100.64.0.1": true, "100.100.100.200": true, "224.0.0.1": true, "255.255.255.255": true,
		"0.1.2.3": true, "240.0.0.1": true, "198.18.0.1": true, "198.19.255.255": true, "192.0.0.192": true,
		"64:ff9b::7f00:1": true, "64:ff9b::a00:1": true, "64:ff9b::a9fe:a9fe": true, "64:ff9b::808:808": false,
		"2002:7f00:1::": true, "2002:a9fe:a9fe::1": true, "2002:808:808::1": false, "2001:0:4136:e378:8000:63bf:3fff:fdd2": true,
		"64:ff9b:1::1": true, "fec0::1": true, "feff::1": true, "198.20.0.1": false, "239.255.255.255": true,
		"93.184.216.34": false, "8.8.8.8": false, "2606:4700:4700::1111": false, "::ffff:8.8.8.8": false, "100.128.0.1": false,
	}
	for s, want := range tests {
		assert.Equal(t, want, IsBlockedIP(net.ParseIP(s)), s)
	}
	assert.True(t, IsBlockedIP(nil))
}

func TestDialContextRefusesInternalTargets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	for _, addr := range []string{srv.Listener.Addr().String(), "localhost:80", "0.0.0.0:80", "[::1]:80", "169.254.169.254:80", "[::ffff:127.0.0.1]:80"} {
		_, err := DialContext(context.Background(), "tcp", addr)
		assert.ErrorIs(t, err, ErrPrivateAddress, addr)
	}
	_, err := DialContext(context.Background(), "tcp", "no-port")
	require.Error(t, err)
	_, err = DialContext(context.Background(), "tcp", "nonexistent.invalid:80")
	assert.ErrorIs(t, err, ErrUnreachable)
}

// fakeNet replaces the resolver and the dialer. answers is consumed one entry per lookup
// (the last one repeats); dialled records every address that was dialled.
func fakeNet(t *testing.T, answers ...[]string) (lookups *int, dialled *[]string) {
	t.Helper()
	var mu sync.Mutex
	lookups, dialled = new(int), new([]string)
	oldLookup, oldDial := lookupIP, dialIP
	t.Cleanup(func() { lookupIP, dialIP = oldLookup, oldDial })
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		mu.Lock()
		defer mu.Unlock()
		i := *lookups
		if i >= len(answers) {
			i = len(answers) - 1
		}
		*lookups++
		var ips []net.IP
		for _, s := range answers[i] {
			ips = append(ips, net.ParseIP(s))
		}
		return ips, nil
	}
	dialIP = func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		defer mu.Unlock()
		*dialled = append(*dialled, addr)
		return nil, errors.New("fake dial")
	}
	return lookups, dialled
}

func TestMixedRecordsDialOnlyThePublicAddress(t *testing.T) {
	lookups, dialled := fakeNet(t, []string{"127.0.0.1", "93.184.216.34", "10.0.0.5", "::1"})
	_, err := DialContext(context.Background(), "tcp", "evil.example:443")
	require.Error(t, err)
	assert.Equal(t, []string{"93.184.216.34:443"}, *dialled)
	assert.Equal(t, 1, *lookups, "the name is resolved exactly once per dial")
}

func TestOnlyPrivateRecordsDialNothing(t *testing.T) {
	_, dialled := fakeNet(t, []string{"127.0.0.1", "192.168.0.9"})
	_, err := DialContext(context.Background(), "tcp", "evil.example:80")
	assert.ErrorIs(t, err, ErrPrivateAddress)
	assert.Empty(t, *dialled)
}

// DNS rebinding: the first answer is public, any later one is private. The guard resolves
// once and dials the checked IP, so the private answer is never used.
func TestDNSRebindingCannotReachPrivateAddress(t *testing.T) {
	lookups, dialled := fakeNet(t, []string{"93.184.216.34"}, []string{"127.0.0.1"})
	_, _ = DialContext(context.Background(), "tcp", "rebind.example:80")
	assert.Equal(t, []string{"93.184.216.34:80"}, *dialled)
	assert.Equal(t, 1, *lookups)

	// A second dial sees the private answer and is refused without dialling.
	_, err := DialContext(context.Background(), "tcp", "rebind.example:80")
	assert.ErrorIs(t, err, ErrPrivateAddress)
	assert.Equal(t, []string{"93.184.216.34:80"}, *dialled)
}

// With HTTP_PROXY set the transport must still dial (and check) the target itself.
func TestTransportIgnoresProxyEnvironment(t *testing.T) {
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(k, "http://127.0.0.1:1")
		require.NoError(t, os.Unsetenv("NO_PROXY"))
	}
	t.Setenv("NO_PROXY", "")
	lookups, dialled := fakeNet(t, []string{"93.184.216.34"})
	client := &http.Client{Transport: NewTransport(ErrUnreachable, ErrPrivateAddress)}
	resp, err := client.Get("http://target.example/")
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	assert.Equal(t, []string{"93.184.216.34:80"}, *dialled, "dialled the target, not the proxy")
	assert.Equal(t, 1, *lookups)

	// And a private target is refused even though a (public-looking) proxy is configured.
	_, dialled = fakeNet(t, []string{"10.0.0.1"})
	resp, err = client.Get("http://internal.example/") //nolint:bodyclose // the request fails, there is no body
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, ErrPrivateAddress)
	assert.Empty(t, *dialled)
}
