package types

import (
	"fmt"
	"sync"
)

// MessageID will identify which message we are dealing with in the Peer Wire Protocol
type MessageID byte

const (
	MsgChoke         MessageID = 0
	MsgUnchoke       MessageID = 1
	MsgInterested    MessageID = 2
	MsgNotInterested MessageID = 3
	MsgHave          MessageID = 4
	MsgBitfield      MessageID = 5
	MsgRequest       MessageID = 6
	MsgPiece         MessageID = 7
	MsgCancel        MessageID = 8
	MsgPort          MessageID = 9
	MsgKeepAlive     MessageID = 255
)

const (
	ProtocolString = "BitTorrent protocol"
	ProtocolLength = byte(len(ProtocolString))
)

// Torrent is the type that represents a torrents metadata from its .torrent file
type Torrent struct {
	Announce     string
	AnnounceList [][]string
	CreationDate int64
	Comment      string
	CreatedBy    string
	Encoding     string
	Info         *InfoDictionary
	PieceManager *PieceManager
}

// InfoDictionary represents the Info portion of the metadata in a .torrent file
type InfoDictionary struct {
	PieceLength int
	Pieces      []byte
	Private     *int
	Name        string
	Length      int64
	Files       *[]File
}

// File represents multiple file torrents defined in the .torrent file
type File struct {
	Length int64
	Md5sum *string
	Path   []string
}

// Piece represents a torrent piece
type Piece struct {
	Hash         []byte
	Data         *[]byte
	IsDownloaded bool
	IsClaimed    bool
}

// NewPiece will return a pointer to a new piece
func NewPiece(hash []byte) *Piece {
	return &Piece{
		Hash:         hash,
		Data:         nil,
		IsDownloaded: false,
		IsClaimed:    false,
	}
}

// PieceManager tracks and manages the pieces of a torrent while we download and upload
type PieceManager struct {
	DownloadedCount int
	PieceCount      int
	PieceSize       int

	mu     sync.RWMutex // Use RWMutex for better concurrency
	pieces map[int]*Piece
}

// NewPieceManager creates a piece manager and returns a pointer to it
func NewPieceManager(pieceCount, pieceSize int) *PieceManager {
	return &PieceManager{
		DownloadedCount: 0,
		PieceCount:      pieceCount,
		PieceSize:       pieceSize,

		pieces: make(map[int]*Piece),
	}
}

// Peer represents a connected peer
type Peer struct {
	PeerID    string
	Address   string
	PeerState PeerState
	PieceList []int
}

// NewPeer returns a pointer to a peer type
func NewPeer(peerID, address string) *Peer {
	return &Peer{
		PeerID:    peerID,
		Address:   address,
		PeerState: NewPeerState(),
		PieceList: []int{},
	}
}

// PeerState represents the connection state with a peer
type PeerState struct {
	AmChoking      bool
	AmInterested   bool
	PeerChoking    bool
	PeerInterested bool
}

// NewPeerState returns the starting peer state for any peer when we connect with them
func NewPeerState() PeerState {
	return PeerState{
		AmChoking:      true,
		AmInterested:   false,
		PeerChoking:    true,
		PeerInterested: false,
	}
}

// Handshake represents the handshake message which starts peer communication in the Peer Wire Protocol
type Handshake struct {
	ProtocolStringLength byte
	ProtocolString       string
	Reserved             [8]byte
	Infohash             [20]byte
	PeerID               [20]byte
}

// NewHandshake creates the initial handshake message to send to a peer when connecting
func NewHandshake(infohash []byte, clientID []byte) ([]byte, error) {
	if len(infohash) != 20 {
		return nil, fmt.Errorf("infoHash length is %d, expected 20", len(infohash))
	}
	if len(clientID) != 20 {
		return nil, fmt.Errorf("clientID length is %d, expected 20", len(clientID))
	}

	handshake := &Handshake{
		ProtocolStringLength: ProtocolLength,
		ProtocolString:       ProtocolString,
		Reserved:             [8]byte{}, // Reserved bytes are all zero
		Infohash:             [20]byte(infohash),
		PeerID:               [20]byte(clientID),
	}
	return handshake.SerializeHandshake(), nil
}

// Message represents a parsed message
type Message struct {
	ID      *MessageID // nil for keep-alive message
	Payload []byte
}

// NewMessage returns a Message type given a MessageID and payload
func NewMessage(id *MessageID, payload []byte) Message {
	return Message{
		ID:      id,
		Payload: payload,
	}
}
