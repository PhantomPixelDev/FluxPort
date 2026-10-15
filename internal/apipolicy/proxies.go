package apipolicy

import (
	"fmt"
	"net/netip"
	"strings"
)

func ParseProxies(values []string) ([]netip.Prefix, error) {
	if len(values) > 32 {
		return nil, fmt.Errorf("use at most 32 trusted proxies")
	}
	out := []netip.Prefix{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if ip, err := netip.ParseAddr(value); err == nil {
			ip = ip.Unmap()
			out = append(out, netip.PrefixFrom(ip, ip.BitLen()))
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q must be an IP address or CIDR", value)
		}
		out = append(out, prefix.Masked())
	}
	return out, nil
}

// Walk trusted hops from the socket toward the client. A client-supplied XFF
// prefix cannot override the first untrusted hop appended by a trusted proxy.
func (m *Manager) ClientIP(peer, forwarded string) string {
	remote, err := netip.ParseAddr(peer)
	if err != nil {
		return peer
	}
	remote = remote.Unmap()
	trusted := func(ip netip.Addr) bool {
		for _, prefix := range m.proxies {
			if prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !trusted(remote) || forwarded == "" || len(forwarded) > 2048 {
		return remote.String()
	}
	parts := strings.Split(forwarded, ",")
	if len(parts) > 32 {
		return remote.String()
	}
	chain := make([]netip.Addr, len(parts))
	for i, value := range parts {
		ip, e := netip.ParseAddr(strings.TrimSpace(value))
		if e != nil {
			return remote.String()
		}
		chain[i] = ip.Unmap()
	}
	current := remote
	for i := len(chain) - 1; i >= 0; i-- {
		if !trusted(current) {
			break
		}
		current = chain[i]
	}
	return current.String()
}
