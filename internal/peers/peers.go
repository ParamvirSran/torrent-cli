package peers

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/ParamvirSran/GoTorrent/internal/types"
)

const (
	HandshakeResponseLength = 68
	KeepAliveInterval       = 30 * time.Second
	PeerTimeout             = 3 * time.Minute
	BlockSize               = 16384 // 16 kb
)

// HandlePeerConnection manages a single peer connection
func HandlePeerConnection(pm *types.PieceManager, ctx context.Context, peerID string, infoHash, clientID []byte, peerAddress string) error {
	peerContext, peerCancel := context.WithCancel(ctx)
	defer peerCancel()

	peer := types.NewPeer(peerID, peerAddress)

	handshake, err := types.NewHandshake(infoHash, clientID)
	if err != nil {
		return fmt.Errorf("error creating handshake: %v", err)
	}

	conn, err := connectToPeer(peerContext, peer.Address)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := sendHandshake(conn, handshake); err != nil {
		return err
	}

	if err := receiveHandshakeResponse(peerID, conn, infoHash); err != nil {
		return err
	}

	stopKeepAlive := startKeepAlive(peerContext, conn)
	defer stopKeepAlive()
	return processMessages(peerContext, conn, peer, pm)
}

// connectToPeer establishes a connection to the peer
func connectToPeer(ctx context.Context, address string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %v", address, err)
	}
	return conn, nil
}

// sendHandshake sends a handshake to the peer
func sendHandshake(conn net.Conn, handshake []byte) error {
	if _, err := conn.Write(handshake); err != nil {
		return fmt.Errorf("error writing handshake: %v", err)
	}
	return nil
}

// receiveHandshakeResponse reads and validates the handshake response from the peer
func receiveHandshakeResponse(peerID string, conn net.Conn, infoHash []byte) error {
	response := make([]byte, HandshakeResponseLength)
	conn.SetReadDeadline(time.Now().Add(PeerTimeout))
	n, err := conn.Read(response)
	if err != nil {
		return fmt.Errorf("failed to read handshake response: %v", err)
	}
	if err := types.ValidateHandshakeResponse(response[:n], [20]byte(infoHash)); err != nil {
		return fmt.Errorf("invalid handshake response: %v", err)
	}
	return nil
}

// startKeepAlive starts a goroutine to send keep-alive messages to the peer
func startKeepAlive(ctx context.Context, conn net.Conn) func() {
	ctx, cancel := context.WithCancel(ctx)
	var once sync.Once
	go func() {
		ticker := time.NewTicker(KeepAliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := conn.Write(KeepAliveMessage()); err != nil {
					log.Printf("Error sending keep-alive to peer %s: %v", conn.RemoteAddr(), err)
					once.Do(cancel)
					return
				}
			}
		}
	}()
	return func() { once.Do(cancel) }
}

// processMessages processes incoming messages from the peer
func processMessages(ctx context.Context, conn net.Conn, peer *types.Peer, pm *types.PieceManager) error {
	lastActivity := time.Now()
	for {
		select {
		case <-ctx.Done():
			log.Printf("Disconnecting from peer: %s", peer.Address)
			return nil
		default:
			conn.SetReadDeadline(time.Now().Add(PeerTimeout))
			msg, err := readMessage(conn)
			if err != nil {
				if errors.Is(err, io.EOF) {
					continue
				}
				return fmt.Errorf("error reading message: %v", err)
			}
			if msg.ID != nil {
				handleMessage(conn, ctx, pm, peer, msg)
				lastActivity = time.Now()
			}
			if time.Since(lastActivity) > PeerTimeout {
				return fmt.Errorf("peer %s timed out", peer.Address)
			}
		}
	}
}

// handleMessage handles a received message
func handleMessage(conn net.Conn, ctx context.Context, pm *types.PieceManager, peer *types.Peer, msg types.Message) {
	switch *msg.ID {
	case types.MsgChoke:
		peer.PeerState.PeerChoking = true
	case types.MsgUnchoke:
		peer.PeerState.PeerChoking = false
	case types.MsgInterested:
		peer.PeerState.PeerInterested = true
	case types.MsgNotInterested:
		peer.PeerState.PeerInterested = false
	case types.MsgHave:
		pieceIndex := binary.BigEndian.Uint32(msg.Payload)
		log.Printf("%s - Received HAVE message for piece %d", peer.Address, pieceIndex)
		if pm.ClaimPiece(int(pieceIndex)) {
			worker(peer, ctx, pm, pieceIndex, conn)
		}
	case types.MsgBitfield:
		log.Printf("%s - Received BITFIELD message: %x", peer.Address, msg.Payload)
	case types.MsgRequest:
		index := binary.BigEndian.Uint32(msg.Payload[0:4])
		begin := binary.BigEndian.Uint32(msg.Payload[4:8])
		length := binary.BigEndian.Uint32(msg.Payload[8:12])
		log.Printf("%s - Received REQUEST message for index %d, begin %d, length %d", peer.Address, index, begin, length)
	case types.MsgPiece:
		index := binary.BigEndian.Uint32(msg.Payload[0:4])
		begin := binary.BigEndian.Uint32(msg.Payload[4:8])
		block := msg.Payload[8:]
		log.Printf("%s - Received PIECE message for index %d, begin %d, block length %d", peer.Address, index, begin, len(block))
	case types.MsgCancel:
		index := binary.BigEndian.Uint32(msg.Payload[0:4])
		begin := binary.BigEndian.Uint32(msg.Payload[4:8])
		length := binary.BigEndian.Uint32(msg.Payload[8:12])
		log.Printf("%s - Received CANCEL message for index %d, begin %d, length %d", peer.Address, index, begin, length)
	case types.MsgPort:
		port := binary.BigEndian.Uint16(msg.Payload)
		log.Printf("%s - Received PORT message with port %d", peer.Address, port)
	default:
		log.Printf("%s - Received unknown message ID %d", peer.Address, *msg.ID)
	}
}

// worker downloads a piece from the peer
func worker(peer *types.Peer, ctx context.Context, pm *types.PieceManager, index uint32, conn net.Conn) {
	var offset uint32 = 0
	piece := make([]byte, pm.PieceSize)

	// Send INTERESTED message to the peer
	if _, err := conn.Write(FixedLengthMessage(types.MsgInterested)); err != nil {
		log.Printf("worker: Error sending INTERESTED message: %v", err)
		return
	}
	peer.PeerState.AmInterested = true

	// Wait for UNCHOKE message from the peer
	for {
		msg, err := readMessage(conn)
		if err != nil {
			log.Printf("worker: Error reading UNCHOKE message: %v", err)
			return
		}
		if msg.ID != nil && *msg.ID == types.MsgUnchoke {
			peer.PeerState.PeerChoking = false
			break
		} else if msg.ID != nil && *msg.ID == types.MsgChoke {
			log.Printf("worker: Peer choked, stopping download of piece %d from peer %s", index, conn.RemoteAddr())
			return
		}
	}

	// Download the piece in blocks
	for offset < uint32(pm.PieceSize) {
		select {
		case <-ctx.Done():
			log.Printf("worker: Context canceled, stopping download of piece %d from peer %s", index, conn.RemoteAddr())
			return
		default:
			// Calculate the block size (usually 16 KB, but smaller for the last block)
			blockSize := uint32(BlockSize)
			if offset+blockSize > uint32(pm.PieceSize) {
				blockSize = uint32(pm.PieceSize) - offset
			}

			// Send a REQUEST message for the block
			request := RequestMessage(index, offset, blockSize)
			if _, err := conn.Write(request); err != nil {
				log.Printf("worker: Error sending REQUEST message for piece %d, offset %d: %v", index, offset, err)
				return
			}

			// Read the PIECE message response
			msg, err := readMessage(conn)
			if err != nil {
				log.Printf("worker: Error reading PIECE message for piece %d, offset %d: %v", index, offset, err)
				return
			}

			// Handle the PIECE message
			if msg.ID != nil && *msg.ID == types.MsgPiece {
				receivedIndex := binary.BigEndian.Uint32(msg.Payload[0:4])
				receivedBegin := binary.BigEndian.Uint32(msg.Payload[4:8])
				block := msg.Payload[8:]

				// Verify the block
				if receivedIndex != index || receivedBegin != offset {
					log.Printf("worker: Mismatch in received block: expected index %d, offset %d; got index %d, offset %d",
						index, offset, receivedIndex, receivedBegin)
					return
				}

				// Copy the block into the piece
				copy(piece[offset:offset+blockSize], block)
				offset += blockSize

			} else if msg.ID != nil && *msg.ID == types.MsgChoke {
				// Handle peer choking
				peer.PeerState.PeerChoking = true
				log.Printf("worker: Peer choked, stopping download of piece %d from peer %s", index, conn.RemoteAddr())
				return
			} else {
				log.Printf("worker: Unexpected message ID %d", *msg.ID)
				return
			}
		}
	}

	// Mark the piece as downloaded and verify it
	pm.MarkPieceDownloaded(int(index), piece)
	if err := pm.VerifyPiece(int(index)); err != nil {
		log.Printf("worker: Verification failed for piece %d: %v", index, err)
		pm.RequeuePiece(int(index))
	} else {
		log.Printf("worker: Successfully downloaded and verified piece %d from peer %s", index, conn.RemoteAddr())
	}
}

// readMessage reads a message from a connection
func readMessage(conn net.Conn) (types.Message, error) {
	var length uint32
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		return types.Message{}, err
	}
	if length == 0 {
		return types.Message{ID: nil, Payload: nil}, nil // Keep-alive message
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return types.Message{}, err
	}
	if len(buf) < 1 {
		return types.Message{}, fmt.Errorf("invalid message: no message ID")
	}

	messageId := types.MessageID(buf[0])
	return types.Message{
		ID:      &messageId,
		Payload: buf[1:],
	}, nil
}
