package browserauth

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

func ValidateCanonicalOrigin(raw string, production bool) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", errors.New("browser assisted origin is required without surrounding whitespace")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("browser assisted origin is invalid")
	}
	canonical := u.Scheme + "://" + u.Host
	if raw != canonical {
		return "", errors.New("browser assisted origin must be canonical scheme://host without a path")
	}
	if production && u.Scheme != "https" {
		return "", errors.New("browser assisted origin must use HTTPS in production")
	}
	if u.Scheme != "https" {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if !(!production && u.Scheme == "http" && (strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback()))) {
			return "", errors.New("insecure browser assisted origin is allowed only for non-production loopback development")
		}
	}
	return canonical, nil
}

func ParseTrustedProxyCIDRs(values []string) ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, errors.New("browser assisted trusted proxy CIDR is invalid")
		}
		out = append(out, network)
	}
	return out, nil
}

func ClientSource(remoteAddr, forwardedFor string, trustedProxies []*net.IPNet) (string, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	remoteIP := net.ParseIP(strings.TrimSpace(host))
	if remoteIP == nil {
		return "", errors.New("browser session exchange peer address is invalid")
	}
	remote := remoteIP.String()
	if !ipInNetworks(remoteIP, trustedProxies) || strings.TrimSpace(forwardedFor) == "" {
		return remote, nil
	}

	parts := strings.Split(forwardedFor, ",")
	ips := make([]net.IP, 0, len(parts))
	for _, part := range parts {
		ip := net.ParseIP(strings.TrimSpace(part))
		if ip == nil {
			return "", errors.New("trusted proxy supplied an invalid forwarded client address")
		}
		ips = append(ips, ip)
	}
	for index := len(ips) - 1; index >= 0; index-- {
		if !ipInNetworks(ips[index], trustedProxies) {
			return ips[index].String(), nil
		}
	}
	return ips[0].String(), nil
}

func ipInNetworks(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
