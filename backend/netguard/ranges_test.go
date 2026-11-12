package netguard

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBlockedIPUpstreamCases(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"127.0.0.1", true},
		{"127.5.5.5", true},
		{"::1", true},
		{"::ffff:127.0.0.1", true}, // IPv4-mapped form must not slip past the IPv4 checks
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"::ffff:192.168.1.1", true},
		{"fc00::1", true},
		{"fd00:ec2::254", true}, // AWS IPv6 metadata endpoint
		{"169.254.169.254", true},
		{"::ffff:169.254.169.254", true},
		{"fe80::1", true},
		{"224.0.0.1", true},
		{"ff02::1", true},
		{"0.0.0.0", true},
		{"::", true},
		{"100.64.0.1", true},
		{"100.100.100.200", true}, // Alibaba metadata endpoint
		{"100.127.255.255", true},

		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"172.15.255.255", false},
		{"172.32.0.1", false},
		{"100.63.255.255", false},
		{"100.128.0.1", false},
		{"2606:4700:4700::1111", false},
		{"64:ff9b::808:808", false}, // NAT64 embedding 8.8.8.8, which DNS64 networks resolve to
		{"2002:808:808::", false},   // 6to4 embedding 8.8.8.8
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip)
			assert.Equal(t, tt.private, IsBlockedIP(ip))
		})
	}

	t.Run("nil is treated as private", func(t *testing.T) {
		assert.True(t, IsBlockedIP(nil))
	})
}

func TestIsBlockedIPReservedRangesThatReachInternalHosts(t *testing.T) {
	for _, s := range []string{
		"64:ff9b::a9fe:a9fe", // NAT64 embedding 169.254.169.254
		"64:ff9b::7f00:1",    // NAT64 embedding 127.0.0.1
		"2002:7f00:1::",      // 6to4 embedding 127.0.0.1
		"::7f00:1",           // IPv4-compatible embedding 127.0.0.1
		"::ffff:0:a00:1",     // IPv4-translated embedding 10.0.0.1
		"fec0::1",            // site-local
		"0.0.0.1",            // "this network"
		"192.0.0.192",        // IETF protocol assignments
		"198.18.0.1",         // benchmarking
		"240.0.0.1",          // reserved
		"255.255.255.255",    // broadcast
		"239.1.1.1",          // multicast
	} {
		assert.True(t, IsBlockedIP(net.ParseIP(s)), s)
	}
}
