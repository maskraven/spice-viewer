// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/maskraven/virt-viewer/internal/connector"
	"github.com/maskraven/virt-viewer/internal/protocol"
	"github.com/maskraven/virt-viewer/internal/ux"
)

// mapDialError classifies connector dial / TLS / proxy failures into ux classes.
func mapDialError(err error) error {
	if err == nil {
		return nil
	}
	var uxErr *ux.Error
	if errors.As(err, &uxErr) {
		return err
	}

	// TLS pin / chain failures from connector.
	if errors.Is(err, connector.ErrTLSVerify) {
		if strings.Contains(err.Error(), "subject does not match") {
			return ux.New(ux.ClassTLSSubject, ux.MsgTLSSubject, err)
		}
		return ux.New(ux.ClassTLSTrust, ux.MsgTLSTrust, err)
	}

	// Pre-dial validation → Config.
	switch {
	case errors.Is(err, connector.ErrEmptyHost),
		errors.Is(err, connector.ErrInvalidHost),
		errors.Is(err, connector.ErrInvalidPort),
		errors.Is(err, connector.ErrMissingRootCAs),
		errors.Is(err, connector.ErrMissingTLSIdentity),
		errors.Is(err, connector.ErrCleartextDenied),
		errors.Is(err, connector.ErrUnsupportedProxy),
		errors.Is(err, connector.ErrInvalidProxy):
		return ux.New(ux.ClassConfig, ux.MsgConfigNotSpice, err)
	}

	msg := err.Error()
	// CONNECT / proxy path.
	if strings.Contains(msg, "CONNECT") || strings.Contains(msg, "proxy") || strings.Contains(msg, "Proxy") {
		return ux.New(ux.ClassProxy, ux.MsgProxy, err)
	}
	// TLS handshake (not already ErrTLSVerify) — trust/chain class.
	if strings.Contains(msg, "TLS") || strings.Contains(msg, "tls") || strings.Contains(msg, "certificate") {
		if strings.Contains(msg, "subject") {
			return ux.New(ux.ClassTLSSubject, ux.MsgTLSSubject, err)
		}
		return ux.New(ux.ClassTLSTrust, ux.MsgTLSTrust, err)
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ux.New(ux.ClassTransport, ux.MsgTransport, err)
	}

	var ne net.Error
	if errors.As(err, &ne) {
		return ux.New(ux.ClassTransport, ux.MsgTransport, err)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return ux.New(ux.ClassTransport, ux.MsgTransport, err)
	}

	return ux.New(ux.ClassTransport, ux.MsgTransport, err)
}

// mapLinkResult maps a non-OK SpiceLinkErr after ticket auth to ClassTicket.
// PR 06 acceptance: bad password / bad link result → Ticket class.
func mapLinkResult(code uint32, cause error) error {
	if cause == nil {
		cause = fmt.Errorf("spice link error %d", code)
	}
	// All post-auth link failures are treated as ticket/auth for UX stability
	// (Proxmox tickets expire quickly; spice-server returns PERMISSION_DENIED
	// or ERROR for bad/expired tickets).
	switch code {
	case protocol.LinkErrPermissionDenied, protocol.LinkErrError,
		protocol.LinkErrInvalidData, protocol.LinkErrBadConnectionID:
		return ux.New(ux.ClassTicket, ux.MsgTicket, cause)
	case protocol.LinkErrNeedSecured, protocol.LinkErrNeedUnsecured:
		return ux.New(ux.ClassConfig, ux.MsgConfigNotSpice, cause)
	case protocol.LinkErrVersionMismatch, protocol.LinkErrInvalidMagic:
		return ux.New(ux.ClassInternal, ux.MsgInternal, cause)
	case protocol.LinkErrChannelNotAvailable:
		return ux.New(ux.ClassInternal, ux.MsgInternal, cause)
	default:
		return ux.New(ux.ClassTicket, ux.MsgTicket, cause)
	}
}

// mapTransportErr wraps I/O failures during link as Transport (or preserves ux.Error).
func mapTransportErr(err error) error {
	if err == nil {
		return nil
	}
	var uxErr *ux.Error
	if errors.As(err, &uxErr) {
		return err
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return ux.New(ux.ClassTransport, ux.MsgTransport, err)
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return ux.New(ux.ClassTransport, ux.MsgTransport, err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ux.New(ux.ClassTransport, ux.MsgTransport, err)
	}
	return ux.New(ux.ClassTransport, ux.MsgTransport, err)
}
