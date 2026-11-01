package agent

import (
	"fmt"
	"net"
	"strings"

	"github.com/gin-gonic/gin"
)

func parseTrustedProxies(values []string) ([]*net.IPNet, error) {
	result := []*net.IPNet{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if ip := net.ParseIP(value); ip != nil {
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			value = fmt.Sprintf("%s/%d", ip, bits)
		}
		_, prefix, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy: %q", value)
		}
		result = append(result, prefix)
	}
	return result, nil
}

func (s *Service) secureRequest(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	for _, proxy := range s.trustedProxies {
		if proxy.Contains(ip) {
			// The trusted edge must overwrite this header, never append user input.
			values := c.Request.Header.Values("X-Forwarded-Proto")
			return len(values) == 1 && strings.EqualFold(strings.TrimSpace(values[0]), "https")
		}
	}
	return false
}
