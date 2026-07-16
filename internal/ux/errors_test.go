package ux_test

import (
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/maskraven/virt-viewer/internal/ux"
)

// Stable message table: design doc examples must not drift without an intentional change.
var stableMessages = map[ux.Class][]string{
	ux.ClassTLSSubject: {ux.MsgTLSSubject},
	ux.ClassTLSTrust:   {ux.MsgTLSTrust},
	ux.ClassProxy:      {ux.MsgProxy},
	ux.ClassTicket:     {ux.MsgTicket},
	ux.ClassTransport:  {ux.MsgTransport},
	ux.ClassConfig:     {ux.MsgConfigNotSpice, ux.MsgConfigFieldTooLarge},
	ux.ClassInternal:   {ux.MsgInternal},
}

func TestStableClassValues(t *testing.T) {
	want := map[ux.Class]string{
		ux.ClassTLSSubject: "TLSSubject",
		ux.ClassTLSTrust:   "TLSTrust",
		ux.ClassProxy:      "Proxy",
		ux.ClassTicket:     "Ticket",
		ux.ClassTransport:  "Transport",
		ux.ClassConfig:     "Config",
		ux.ClassInternal:   "Internal",
	}
	for c, s := range want {
		if string(c) != s {
			t.Errorf("class %q: string value = %q, want %q", c, string(c), s)
		}
	}
}

func TestStableMessages(t *testing.T) {
	// Exact strings from design / PR acceptance table.
	cases := []struct {
		msg  string
		want string
	}{
		{ux.MsgTLSSubject, "Certificate subject does not match connection file"},
		{ux.MsgTLSTrust, "Cannot validate server certificate"},
		{ux.MsgProxy, "Cannot reach Proxmox spiceproxy"},
		{ux.MsgTicket, "Ticket invalid or expired — open Console again in Proxmox"},
		{ux.MsgTransport, "Connection lost — re-open Console for a new ticket"},
		{ux.MsgConfigNotSpice, "Not a SPICE connection file"},
		{ux.MsgConfigFieldTooLarge, "Connection file rejected (field too large)"},
	}
	for _, tc := range cases {
		if tc.msg != tc.want {
			t.Errorf("message drifted:\n  got  %q\n  want %q", tc.msg, tc.want)
		}
	}
}

func TestNewAndUserMessagePerClass(t *testing.T) {
	for class, msgs := range stableMessages {
		for _, msg := range msgs {
			t.Run(string(class)+"/"+msg, func(t *testing.T) {
				underlying := errors.New("detail")
				e := ux.New(class, msg, underlying)
				if e.Class != class {
					t.Fatalf("Class = %q, want %q", e.Class, class)
				}
				if e.Message != msg {
					t.Fatalf("Message = %q, want %q", e.Message, msg)
				}
				if !errors.Is(e, underlying) {
					t.Fatalf("Unwrap/Is failed for underlying")
				}
				if got := ux.UserMessage(e); got != msg {
					t.Fatalf("UserMessage = %q, want %q", got, msg)
				}
				// Error() must include user message and class for logs.
				s := e.Error()
				if s == "" {
					t.Fatal("Error() empty")
				}
			})
		}
	}
}

func TestNewEmptyMessageUsesDefault(t *testing.T) {
	e := ux.New(ux.ClassTicket, "", nil)
	if e.Message != ux.MsgTicket {
		t.Fatalf("Message = %q, want default %q", e.Message, ux.MsgTicket)
	}
}

func TestClassifyNil(t *testing.T) {
	if got := ux.Classify(nil); got != nil {
		t.Fatalf("Classify(nil) = %v, want nil", got)
	}
	if got := ux.UserMessage(nil); got != "" {
		t.Fatalf("UserMessage(nil) = %q, want empty", got)
	}
}

func TestClassifyIdentity(t *testing.T) {
	e := ux.New(ux.ClassProxy, ux.MsgProxy, errors.New("connect refused"))
	got := ux.Classify(e)
	if got != e {
		t.Fatalf("Classify(*Error) returned different pointer")
	}
	// Wrapped *Error
	wrapped := fmt.Errorf("dial: %w", e)
	got = ux.Classify(wrapped)
	if got == nil || got.Class != ux.ClassProxy || got.Message != ux.MsgProxy {
		t.Fatalf("Classify(wrapped) = %#v", got)
	}
}

func TestClassifyEOFTransport(t *testing.T) {
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, net.ErrClosed} {
		got := ux.Classify(err)
		if got.Class != ux.ClassTransport {
			t.Errorf("Classify(%v).Class = %q, want Transport", err, got.Class)
		}
		if got.Message != ux.MsgTransport {
			t.Errorf("Classify(%v).Message = %q, want %q", err, got.Message, ux.MsgTransport)
		}
		if ux.UserMessage(err) != ux.MsgTransport {
			t.Errorf("UserMessage(%v) = %q", err, ux.UserMessage(err))
		}
	}
}

func TestClassifyUnknownInternal(t *testing.T) {
	err := errors.New("something obscure")
	got := ux.Classify(err)
	if got.Class != ux.ClassInternal {
		t.Fatalf("Class = %q, want Internal", got.Class)
	}
	if got.Message != ux.MsgInternal {
		t.Fatalf("Message = %q, want %q", got.Message, ux.MsgInternal)
	}
	if ux.UserMessage(err) != ux.MsgInternal {
		t.Fatalf("UserMessage = %q", ux.UserMessage(err))
	}
}

func TestErrorsIsByClass(t *testing.T) {
	a := ux.New(ux.ClassTicket, ux.MsgTicket, nil)
	b := ux.New(ux.ClassTicket, "other wording", errors.New("x"))
	if !errors.Is(a, b) {
		t.Fatal("errors.Is should match same Class")
	}
	c := ux.New(ux.ClassProxy, ux.MsgProxy, nil)
	if errors.Is(a, c) {
		t.Fatal("errors.Is must not match different Class")
	}
}

func TestConditionTable(t *testing.T) {
	// Design condition → class + example message.
	type row struct {
		cond string
		err  *ux.Error
	}
	rows := []row{
		{"TLS subject mismatch", ux.New(ux.ClassTLSSubject, ux.MsgTLSSubject, nil)},
		{"Chain/ca fail", ux.New(ux.ClassTLSTrust, ux.MsgTLSTrust, nil)},
		{"CONNECT failure", ux.New(ux.ClassProxy, ux.MsgProxy, nil)},
		{"Link error bad password", ux.New(ux.ClassTicket, ux.MsgTicket, nil)},
		{"Child channel ticket fail", ux.New(ux.ClassTicket, ux.MsgTicket, nil)},
		{"EOF mid-session", ux.New(ux.ClassTransport, ux.MsgTransport, io.EOF)},
		{"Unsupported type", ux.New(ux.ClassConfig, ux.MsgConfigNotSpice, nil)},
		{"Parser limit", ux.New(ux.ClassConfig, ux.MsgConfigFieldTooLarge, nil)},
	}
	for _, r := range rows {
		t.Run(r.cond, func(t *testing.T) {
			if got := ux.UserMessage(r.err); got != r.err.Message {
				t.Fatalf("UserMessage = %q, want %q", got, r.err.Message)
			}
			cl := ux.Classify(r.err)
			if cl.Class != r.err.Class {
				t.Fatalf("Class = %q, want %q", cl.Class, r.err.Class)
			}
		})
	}
}
