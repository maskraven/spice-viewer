package connector

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"time"
)

// ErrTLSVerify is returned when peer certificate verification fails.
var ErrTLSVerify = errors.New("connector: TLS peer verification failed")

// buildTLSConfig constructs a tls.Config for the given TLSParams.
//
// Proxmox / subject-pin mode (HostSubject non-empty):
//   - InsecureSkipVerify is true only to skip hostname checks
//   - VerifyPeerCertificate performs chain verify against RootCAs + DN pin
//   - ServerName is intentionally left empty (must not be the pvespiceproxy token)
//
// Direct DNS mode (HostSubject empty):
//   - Standard verification with ServerName and RootCAs
func buildTLSConfig(params *TLSParams) (*tls.Config, error) {
	if params == nil {
		return nil, errors.New("connector: nil TLSParams")
	}
	if params.RootCAs == nil {
		return nil, ErrMissingRootCAs
	}

	if params.HostSubject != "" {
		hostSubject := params.HostSubject
		roots := params.RootCAs
		return &tls.Config{
			// Skip hostname only; chain + DN checked in VerifyPeerCertificate.
			InsecureSkipVerify: true, //nolint:gosec // intentional; custom verify below
			RootCAs:            roots,
			// ServerName MUST NOT be the pvespiceproxy token.
			ServerName: "",
			MinVersion: tls.VersionTLS12,
			VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				return verifyPeerCertificate(rawCerts, roots, hostSubject)
			},
		}, nil
	}

	if params.ServerName == "" {
		return nil, ErrMissingTLSIdentity
	}
	return &tls.Config{
		RootCAs:    params.RootCAs,
		ServerName: params.ServerName,
		MinVersion: tls.VersionTLS12,
	}, nil
}

// verifyPeerCertificate implements the design-doc TLS pin algorithm:
//
//  1. Reject empty chain
//  2. Parse leaf + intermediates
//  3. leaf.Verify with Roots from .vv CA only, KeyUsages ServerAuth, no DNSName
//  4. If hostSubject set: subjectMatches on leaf.Subject
func verifyPeerCertificate(rawCerts [][]byte, roots *x509.CertPool, hostSubject string) error {
	if len(rawCerts) == 0 {
		return fmt.Errorf("%w: empty peer certificate chain", ErrTLSVerify)
	}
	certs := make([]*x509.Certificate, 0, len(rawCerts))
	for i, raw := range rawCerts {
		c, err := x509.ParseCertificate(raw)
		if err != nil {
			return fmt.Errorf("%w: parse certificate[%d]: %v", ErrTLSVerify, i, err)
		}
		certs = append(certs, c)
	}
	leaf := certs[0]
	intermediates := x509.NewCertPool()
	for _, c := range certs[1:] {
		intermediates.AddCert(c)
	}
	opts := x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		// DNSName intentionally unset in pin mode.
	}
	if _, err := leaf.Verify(opts); err != nil {
		return fmt.Errorf("%w: chain: %v", ErrTLSVerify, err)
	}
	if hostSubject != "" {
		if !subjectMatches(leaf.Subject, hostSubject) {
			return fmt.Errorf("%w: subject does not match host-subject", ErrTLSVerify)
		}
	}
	return nil
}
