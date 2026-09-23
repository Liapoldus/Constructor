package presentation

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

func browserOriginBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackBrowserOrigin(r.Header.Get("Origin")) {
			writeProblem(w, http.StatusForbidden, "browser_origin_forbidden", "The local API accepts browser requests only from loopback origins.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// LoopbackHostBoundary prevents DNS-rebinding hosts from reaching the local API.
// It is installed by the API server around the handler.
func LoopbackHostBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRequestHost(r.Host) {
			id := newRequestID()
			w.Header().Set("X-Request-ID", id)
			writer := &requestMetadataWriter{ResponseWriter: w, requestID: id, instance: r.URL.Path}
			writeProblem(writer, http.StatusForbidden, "request_host_forbidden", "The local API accepts requests addressed to a loopback host only.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackRequestHost(value string) bool {
	host := strings.TrimSpace(value)
	if host == "" {
		return false
	}
	if splitHost, port, err := net.SplitHostPort(host); err == nil {
		parsedPort, err := strconv.Atoi(port)
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return false
		}
		host = splitHost
	} else if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.Zone() == "" && address.IsLoopback()
}
