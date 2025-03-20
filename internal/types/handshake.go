package types

import (
	"bytes"
	"fmt"
)

// SerializeHandshake turns the handshake into a byte slice to send over the wire
func (h *Handshake) SerializeHandshake() []byte {
	buf := new(bytes.Buffer)
	buf.WriteByte(h.ProtocolStringLength)
	buf.WriteString(h.ProtocolString)
	buf.Write(h.Reserved[:])
	buf.Write(h.Infohash[:])
	buf.Write(h.PeerID[:])
	return buf.Bytes()
}

// ValidateHandshakeResponse will check if received handshake is valid
func ValidateHandshakeResponse(response []byte, expectedInfoHash [20]byte) error {
	if len(response) < 68 {
		return fmt.Errorf("handshake response too short: %d bytes", len(response))
	}
	if infoHash := [20]byte(response[28:48]); !bytes.Equal(infoHash[:], expectedInfoHash[:]) {
		return fmt.Errorf("invalid info hash: expected %x, got %x", expectedInfoHash, infoHash)
	}
	return nil
}
