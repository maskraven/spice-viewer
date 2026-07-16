package ux

import (
	"errors"
	"fmt"
	"io"
	"net"
)

// Class is a stable error category shared by CLI and GUI.
type Class string

// Error classes for pre-session and session failures.
const (
	ClassTLSSubject Class = "TLSSubject"
	ClassTLSTrust   Class = "TLSTrust"
	ClassProxy      Class = "Proxy"
	ClassTicket     Class = "Ticket"
	ClassTransport  Class = "Transport"
	ClassConfig     Class = "Config"
	ClassInternal   Class = "Internal"
)

// Canonical user-facing messages. Producers should reuse these for stability.
const (
	MsgTLSSubject          = "Certificate subject does not match connection file"
	MsgTLSTrust            = "Cannot validate server certificate"
	MsgProxy               = "Cannot reach Proxmox spiceproxy"
	MsgTicket              = "Ticket invalid or expired — open Console again in Proxmox"
	MsgTransport           = "Connection lost — re-open Console for a new ticket"
	MsgConfigNotSpice      = "Not a SPICE connection file"
	MsgConfigFieldTooLarge = "Connection file rejected (field too large)"
	// MsgConfigEndpoint covers dial/setup validation (cleartext policy, missing CA,
	// bad host/port, unsupported proxy) — Class is Config; Message is best-effort.
	MsgConfigEndpoint = "Connection settings are invalid or incomplete"
	// MsgConfigProtocol is used when the peer does not speak SPICE (bad magic/version).
	MsgConfigProtocol = "Peer is not a SPICE server"
	MsgInternal       = "An unexpected error occurred"
)

// Error is a classified, user-facing error.
// Class and Message are stable for UI mapping; Err is optional underlying detail.
type Error struct {
	Class   Class
	Message string // user-facing
	Err     error  // underlying, optional
}

// New builds a classified error. msg should be a stable user-facing string
// (prefer the Msg* constants). err may be nil.
func New(class Class, msg string, err error) *Error {
	if msg == "" {
		msg = defaultMessage(class)
	}
	return &Error{Class: class, Message: msg, Err: err}
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err != nil {
		return fmt.Sprintf("%s (%s): %v", e.Message, e.Class, e.Err)
	}
	if e.Message != "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Class)
	}
	return string(e.Class)
}

// Unwrap returns the underlying error, if any.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is reports whether target is an *Error with the same Class.
// Message and underlying Err are not compared.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok || e == nil || t == nil {
		return false
	}
	return e.Class == t.Class
}

// Classify maps err to a classified *Error when possible.
// If err is already (or wraps) an *Error, that value is returned.
// Otherwise a best-effort class is chosen; unknown errors become ClassInternal.
// Classify(nil) returns nil.
func Classify(err error) *Error {
	if err == nil {
		return nil
	}
	var uxErr *Error
	if errors.As(err, &uxErr) {
		return uxErr
	}

	// Transport: EOF / closed connection mid-session.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return New(ClassTransport, MsgTransport, err)
	}

	// net.Error timeouts and common dial failures → transport (not proxy-specific
	// without CONNECT context; callers should New(ClassProxy, ...) for CONNECT).
	var ne net.Error
	if errors.As(err, &ne) {
		return New(ClassTransport, MsgTransport, err)
	}

	return New(ClassInternal, MsgInternal, err)
}

// UserMessage returns a stable user-facing string for err.
// Nil err yields an empty string.
func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	if e := Classify(err); e != nil && e.Message != "" {
		return e.Message
	}
	return MsgInternal
}

// defaultMessage returns the primary canonical message for class.
// ClassConfig has more than one message; returns MsgConfigNotSpice as a default.
func defaultMessage(c Class) string {
	switch c {
	case ClassTLSSubject:
		return MsgTLSSubject
	case ClassTLSTrust:
		return MsgTLSTrust
	case ClassProxy:
		return MsgProxy
	case ClassTicket:
		return MsgTicket
	case ClassTransport:
		return MsgTransport
	case ClassConfig:
		return MsgConfigNotSpice
	case ClassInternal:
		return MsgInternal
	default:
		return MsgInternal
	}
}
