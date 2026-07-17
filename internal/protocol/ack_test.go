// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestAckState_WindowedAcks(t *testing.T) {
	var buf bytes.Buffer
	var a AckState

	body := make([]byte, 8)
	binary.LittleEndian.PutUint32(body[0:4], 7)
	binary.LittleEndian.PutUint32(body[4:8], 3)
	if err := a.OnSetAck(&buf, body); err != nil {
		t.Fatal(err)
	}
	syncMsg, err := ReadMessage(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if syncMsg.Type != MsgcAckSync {
		t.Fatalf("type=%d want ACK_SYNC %d", syncMsg.Type, MsgcAckSync)
	}
	if len(syncMsg.Data) < 4 || binary.LittleEndian.Uint32(syncMsg.Data) != 7 {
		t.Fatalf("generation body=%v", syncMsg.Data)
	}

	buf.Reset()
	for i := 0; i < 3; i++ {
		if err := a.AfterRead(&buf); err != nil {
			t.Fatal(err)
		}
	}
	ackMsg, err := ReadMessage(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if ackMsg.Type != MsgcAck {
		t.Fatalf("type=%d want ACK %d", ackMsg.Type, MsgcAck)
	}
	if len(ackMsg.Data) != 0 {
		t.Fatalf("ACK body should be empty, got %d", len(ackMsg.Data))
	}

	buf.Reset()
	for i := 0; i < 3; i++ {
		if err := a.AfterRead(&buf); err != nil {
			t.Fatal(err)
		}
	}
	if buf.Len() == 0 {
		t.Fatal("expected second ACK")
	}
}
