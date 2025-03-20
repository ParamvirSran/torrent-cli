package types

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"log"
)

// AddPiece adds a piece to the piece manager
func (pm *PieceManager) AddPiece(index int, hash []byte) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pieces[index] = NewPiece(hash)
}

// RequeuePiece will requeue a piece at an index
func (pm *PieceManager) RequeuePiece(index int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if piece, exists := pm.pieces[index]; exists {
		if piece.IsDownloaded {
			pm.DownloadedCount--
		}
		piece.IsDownloaded = false
		piece.IsClaimed = false
		piece.Data = nil // Clear the pointer to avoid memory leaks
		log.Printf("Piece %d re-queued for download", index)
	}
}

// IsDownloadComplete checks if all pieces are downloaded
func (pm *PieceManager) IsDownloadComplete() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.DownloadedCount == len(pm.pieces) {
		log.Println("Download complete!")
		return true
	}
	return false
}

// ClaimPiece marks a piece as claimed by a worker for download
func (pm *PieceManager) ClaimPiece(index int) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if piece, exists := pm.pieces[index]; exists {
		if !piece.IsDownloaded && !piece.IsClaimed {
			piece.IsClaimed = true
			log.Printf("Piece %d claimed", index)
			return true
		}
	}
	return false
}

// MarkPieceDownloaded marks a piece as downloaded
func (pm *PieceManager) MarkPieceDownloaded(index int, data []byte) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if piece, exists := pm.pieces[index]; exists {
		if !piece.IsDownloaded {
			dataCopy := make([]byte, len(data))
			copy(dataCopy, data)
			piece.Data = &dataCopy
			piece.IsDownloaded = true
			piece.IsClaimed = false
			pm.DownloadedCount++
			log.Printf("Piece %d marked as downloaded: %x", index, *piece.Data)
		}
	}
}

// VerifyPiece verifies that the piece is downloaded and the hash matches the expected hash
func (pm *PieceManager) VerifyPiece(index int) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	piece, exists := pm.pieces[index]
	if !exists {
		return fmt.Errorf("piece %d does not exist", index)
	}
	if !piece.IsDownloaded || piece.Data == nil {
		return fmt.Errorf("piece %d is not downloaded or data is nil", index)
	}

	hasher := sha1.New()
	hasher.Write(*piece.Data)
	computedHash := hasher.Sum(nil)

	if !bytes.Equal(computedHash, piece.Hash) {
		return fmt.Errorf("piece %d hash does not match the expected hash", index)
	}
	log.Printf("Piece:%d, Verified for Integrity", index)
	return nil
}
