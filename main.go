package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ParamvirSran/GoTorrent/internal/peers"
	"github.com/ParamvirSran/GoTorrent/internal/torrent"
	"github.com/ParamvirSran/GoTorrent/internal/types"
)

const (
	defaultPort = "6881"
	startEvent  = "started"
)

type TorrentStats struct {
	PieceSize  int
	PieceCount int
	Downloaded int
	Uploaded   int
	TotalSize  int
	Left       int
}

func main() {
	logFile, err := setupLogging()
	if err != nil {
		fmt.Printf("Failed to open log file: %v", err)
		os.Exit(1)
	}
	defer logFile.Close()
	log.SetOutput(logFile)
	log.Println("Starting")

	torrentPath := parseArgs()
	torrentStats, torrentFile, infohash, peerID, err := initializeTorrent(torrentPath)
	if err != nil {
		fmt.Printf("Failed to initialize torrent: %v", err)
		os.Exit(1)
	}

	peerIDList, peerAddressList, err := getPeers(torrentStats, torrentFile, infohash, peerID) // Pass pointer
	if err != nil {
		fmt.Printf("Failed to get peers: %v", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go peerManager(torrentFile, ctx, peerIDList, peerAddressList, infohash, peerID)
	go monitorDownloadCompletion(ctx, cancel, torrentFile, torrentStats) // Pass pointer

	<-ctx.Done()
	log.Printf("Exiting. Context finished with error: %v", ctx.Err())
}

func setupLogging() (*os.File, error) {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	logFile, err := os.OpenFile("gotorrent.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %v", err)
	}
	return logFile, nil
}

func parseArgs() string {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <torrent-file>", os.Args[0])
		os.Exit(1)
	}
	return os.Args[1]
}

func initializeTorrent(torrentPath string) (*TorrentStats, *types.Torrent, []byte, []byte, error) {
	torrentFile, err := torrent.ParseTorrentFile(torrentPath)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("error parsing torrent file (%s): %w", torrentPath, err)
	}

	torrentStats := TorrentStats{
		PieceSize:  torrentFile.Info.PieceLength,
		PieceCount: len(torrentFile.Info.Pieces) / 20,
		Downloaded: 0,
		Uploaded:   0,
		TotalSize:  torrentFile.Info.PieceLength * len(torrentFile.Info.Pieces) / 20,
		Left:       torrentFile.Info.PieceLength * len(torrentFile.Info.Pieces) / 20,
	}

	infohash, err := torrent.GetInfohash(torrentFile.Info)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("error getting infohash: %w", err)
	}

	peerID, err := torrent.GeneratePeerID()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("error generating peerID: %w", err)
	}
	return &torrentStats, torrentFile, infohash, []byte(peerID), nil
}

func getPeers(ts *TorrentStats, torrentFile *types.Torrent, infoHash, peerID []byte) ([]string, []string, error) {
	trackerList := torrent.GatherTrackers(torrentFile)
	if len(trackerList) == 0 {
		return nil, nil, fmt.Errorf("no valid trackers found")
	}

	peerIDList, peerAddressList, err := torrent.ContactTrackers(trackerList, string(infoHash), string(peerID), startEvent, ts.Uploaded, ts.Downloaded, ts.Left, defaultPort)
	if err != nil {
		return nil, nil, fmt.Errorf("error contacting trackers: %w", err)
	}

	if len(peerAddressList) == 0 {
		return nil, nil, fmt.Errorf("no peers found from trackers")
	}
	return peerIDList, peerAddressList, nil
}

func peerManager(torrentFile *types.Torrent, ctx context.Context, peerIDList, peerAddressList []string, infohash, clientID []byte) {
	var wg sync.WaitGroup
	pm := torrentFile.PieceManager

	for i := range peerAddressList {
		select {
		case <-ctx.Done():
			log.Println("Context canceled, stopping peer connections.")
			return

		default:
			wg.Add(1)

			go func(peerID, peerAddress string) {
				defer wg.Done()

				if err := peers.HandlePeerConnection(pm, ctx, peerID, infohash, clientID, peerAddress); err != nil {
					log.Printf("Failed with Peer: %s - %v", peerAddress, err)
				}
			}(peerIDList[i], peerAddressList[i])
		}
	}
	wg.Wait()
	log.Println("All peer connections finished. Peer manager finished")
}

func monitorDownloadCompletion(ctx context.Context, cancel context.CancelFunc, torrentFile *types.Torrent, ts *TorrentStats) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if torrentFile.PieceManager.IsDownloadComplete() {
				log.Println("Torrent download complete. Stopping download manager.")
				cancel() // Graceful shutdown
				return
			}
			ts.Downloaded = torrentFile.PieceManager.DownloadedCount * ts.PieceSize
			ts.Left = ts.TotalSize - ts.Downloaded
			log.Printf("Torrent Status: Pieces: %d - Downloaded = %d bytes - Total: %d bytes - Left: %d bytes", torrentFile.PieceManager.DownloadedCount, ts.Downloaded, ts.TotalSize, ts.Left)
		case <-ctx.Done():
			log.Println("Monitor exiting due to context cancellation.")
			return
		}
	}
}
