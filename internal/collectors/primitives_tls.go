package collectors

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"time"
)

// sslCertExpiry connects TLS to host:port and returns the number of whole days
// until the leaf certificate's NotAfter. Negative means already expired. Catches
// cert-expiry outages before they happen — alert on e.g. `< 14`. Cross-platform
// (pure crypto/tls), so it also works once the agent ships for Windows. (#40 W1)
//
// Verification is intentionally skipped: the goal is to READ the expiry date,
// which must work even for self-signed, mismatched, or already-expired certs.
//
// Args:
//
//	host:        string  required — what to dial
//	port:        number  optional, default 443
//	server_name: string  optional — SNI to select the cert (default = host)
//	timeout_sec: number  optional, default 5
func sslCertExpiry(ctx context.Context, args map[string]any) (any, error) {
	host, _ := args["host"].(string)
	if host == "" {
		return nil, fmt.Errorf("host: required")
	}
	port := int(argFloat(args, "port", 443))
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("port: must be 1..65535")
	}
	serverName, _ := args["server_name"].(string)
	if serverName == "" {
		serverName = host
	}
	timeoutSec := argFloat(args, "timeout_sec", 5)
	cctx, cancel := context.WithTimeout(ctx, time.Duration((timeoutSec+1)*float64(time.Second)))
	defer cancel()

	dialer := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: time.Duration(timeoutSec * float64(time.Second))},
		Config: &tls.Config{
			ServerName:         serverName,
			InsecureSkipVerify: true, // we read NotAfter ourselves; chain validity isn't the metric
			MinVersion:         tls.VersionTLS12,
		},
	}
	conn, err := dialer.DialContext(cctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("tls dial %s:%d: %w", host, port, err)
	}
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return nil, fmt.Errorf("not a tls connection")
	}
	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, fmt.Errorf("no peer certificate presented by %s:%d", host, port)
	}
	// The leaf (server) certificate is always first.
	return int(time.Until(certs[0].NotAfter).Hours() / 24), nil
}
