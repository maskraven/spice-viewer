// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"

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

	// TLS subject pin failure (also wraps ErrTLSVerify).
	if errors.Is(err, connector.ErrTLSSubjectMismatch) {
		return ux.New(ux.ClassTLSSubject, ux.MsgTLSSubject, err)
	}
	// TLS pin / chain failures from connector (and handshake wrapping them).
	if errors.Is(err, connector.ErrTLSVerify) || errors.Is(err, connector.ErrTLSHandshake) {
		return ux.New(ux.ClassTLSTrust, ux.MsgTLSTrust, err)
	}

	// CONNECT / proxy path (typed sentinels).
	if errors.Is(err, connector.ErrCONNECT) || errors.Is(err, connector.ErrProxyDial) {
		return ux.New(ux.ClassProxy, ux.MsgProxy, err)
	}

	// Pre-dial validation → Config with dial-setup message (not MsgConfigNotSpice).
	switch {
	case errors.Is(err, connector.ErrEmptyHost),
		errors.Is(err, connector.ErrInvalidHost),
		errors.Is(err, connector.ErrInvalidPort),
		errors.Is(err, connector.ErrMissingRootCAs),
		errors.Is(err, connector.ErrMissingTLSIdentity),
		errors.Is(err, connector.ErrCleartextDenied),
		errors.Is(err, connector.ErrUnsupportedProxy),
		errors.Is(err, connector.ErrInvalidProxy):
		return ux.New(ux.ClassConfig, ux.MsgConfigEndpoint, err)
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
		return ux.New(ux.ClassConfig, ux.MsgConfigEndpoint, cause)
	case protocol.LinkErrVersionMismatch, protocol.LinkErrInvalidMagic:
		return ux.New(ux.ClassConfig, ux.MsgConfigProtocol, cause)
	case protocol.LinkErrChannelNotAvailable:
		return ux.New(ux.ClassInternal, ux.MsgInternal, cause)
	default:
		return ux.New(ux.ClassTicket, ux.MsgTicket, cause)
	}
}

// mapLinkIOErr classifies I/O during the link handshake.
// Context cancel/deadline → Transport; protocol framing → Config; EOF → Transport.
func mapLinkIOErr(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var uxErr *ux.Error
	if errors.As(err, &uxErr) {
		return err
	}
	// Prefer ctx.Err when cancel/deadline unblocked I/O via SetDeadline(past).
	if ctx != nil && ctx.Err() != nil {
		return mapTransportErr(ctx.Err())
	}
	if isProtocolFramingErr(err) {
		return ux.New(ux.ClassConfig, ux.MsgConfigProtocol, err)
	}
	return mapTransportErr(err)
}

// isProtocolFramingErr reports link-header / reply validation failures (not plain I/O).
func isProtocolFramingErr(err error) bool {
	return errors.Is(err, protocol.ErrInvalidMagic) ||
		errors.Is(err, protocol.ErrVersionMismatch) ||
		errors.Is(err, protocol.ErrLinkReplyTooLarge)
}

// mapTransportErr wraps I/O failures as Transport (or preserves ux.Error).
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
