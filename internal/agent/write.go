// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"encoding/binary"
	"io"
)

func writeMini(w io.Writer, typ uint16, body []byte) error {
	hdr := make([]byte, 6)
	binary.LittleEndian.PutUint16(hdr[0:2], typ)
	binary.LittleEndian.PutUint32(hdr[2:6], uint32(len(body)))
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	_, err := w.Write(body)
	return err
}
