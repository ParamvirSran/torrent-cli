package torrent

import (
	"bytes"
	"fmt"
	"os"

	"github.com/ParamvirSran/GoTorrent/internal/bencode"
	"github.com/ParamvirSran/GoTorrent/internal/types"
)

const (
	_keyAnnounce     = "announce"
	_keyAnnounceList = "announce-list"
	_keyInfo         = "info"
	_keyPieceLength  = "piece length"
	_keyPieces       = "pieces"
	_keyLength       = "length"
	_keyFiles        = "files"
	_keyName         = "name"
	_keyComment      = "comment"
	_keyCreatedBy    = "created by"
	_keyEncoding     = "encoding"
	_keyPrivate      = "private"
)

// ParseTorrentFile parses the .torrent file and returns the parsed TorrentFile object
func ParseTorrentFile(torrentPath string) (*types.Torrent, error) {
	content, err := os.ReadFile(torrentPath)
	if err != nil {
		return nil, fmt.Errorf("error reading .torrent file: %w", err)
	}

	reader := bytes.NewReader(content)
	data, err := bencode.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decode torrent file: %w", err)
	}

	torrentDict, ok := data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid torrent file format: expected a dictionary but got %T", data)
	}

	torrent, err := parseMetainfo(torrentDict)
	if err != nil {
		return nil, fmt.Errorf("error parsing metainfo: %v", err)
	}
	return torrent, nil
}

// parseMetainfo parses the metainfo dictionary
func parseMetainfo(torrentDict map[string]any) (*types.Torrent, error) {
	torrentFile := &types.Torrent{}
	if err := parseAnnounce(torrentDict, torrentFile); err != nil {
		return nil, err
	}

	infoDict, ok := torrentDict[_keyInfo].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("info dictionary missing or of incorrect type")
	}

	info, pieceManager, err := parseInfo(infoDict)
	if err != nil {
		return nil, fmt.Errorf("failed to parse info dictionary: %w", err)
	}
	torrentFile.Info = info
	torrentFile.PieceManager = pieceManager

	// Optional Fields
	if comment, ok := torrentDict[_keyComment].(string); ok {
		torrentFile.Comment = comment
	}
	if createdBy, ok := torrentDict[_keyCreatedBy].(string); ok {
		torrentFile.CreatedBy = createdBy
	}
	if encoding, ok := torrentDict[_keyEncoding].(string); ok {
		torrentFile.Encoding = encoding
	}
	if private, ok := torrentDict[_keyPrivate].(int); ok {
		torrentFile.Info.Private = &private
	}
	if creationDate, ok := torrentDict["creation date"].(int64); ok {
		torrentFile.CreationDate = creationDate
	}
	return torrentFile, nil
}

// parseAnnounce parses the "announce" and "announce-list" fields
func parseAnnounce(torrentDict map[string]any, torrentFile *types.Torrent) error {
	if announce, ok := torrentDict[_keyAnnounce].(string); ok {
		torrentFile.Announce = announce
	} else {
		return fmt.Errorf("%s URL missing or not a string", _keyAnnounce)
	}

	if announceList, ok := torrentDict[_keyAnnounceList].([]any); ok {
		for _, tier := range announceList {
			if urlList, ok := tier.([]any); ok {
				var urls []string
				for _, url := range urlList {
					if urlString, ok := url.(string); ok {
						urls = append(urls, urlString)
					} else {
						return fmt.Errorf("%s contains non-string URL: %v", _keyAnnounceList, url)
					}
				}
				torrentFile.AnnounceList = append(torrentFile.AnnounceList, urls)
			} else {
				return fmt.Errorf("%s tier is not a list of URLs: %T", _keyAnnounceList, tier)
			}
		}
	}
	return nil
}

// parseInfo parses the info dictionary from the torrent file
func parseInfo(infoDict map[string]any) (*types.InfoDictionary, *types.PieceManager, error) {
	info := &types.InfoDictionary{}
	if err := parseNameAndPieceLength(infoDict, info); err != nil {
		return nil, nil, err
	}
	if err := parsePiecesField(infoDict, info); err != nil {
		return nil, nil, err
	}
	if err := parseLengthOrFiles(infoDict, info); err != nil {
		return nil, nil, err
	}

	// Initialize PieceManager
	pieceCount := len(info.Pieces) / 20
	pieceLength := info.PieceLength
	pieceManager := types.NewPieceManager(pieceCount, pieceLength)

	// Populate the PieceManager with piece hashes
	for i := 0; i < pieceCount; i += 1 {
		pieceHash := info.Pieces[i*20 : i*20+20]
		pieceManager.AddPiece(i, pieceHash)
	}
	return info, pieceManager, nil
}

// parseNameAndPieceLength parses the name and piece length from the info dictionary
func parseNameAndPieceLength(infoDict map[string]any, info *types.InfoDictionary) error {
	name, ok := infoDict[_keyName].(string)
	if !ok {
		return fmt.Errorf("%s field missing or not a string", _keyName)
	}
	info.Name = name

	switch pl := infoDict[_keyPieceLength].(type) {
	case int:
		info.PieceLength = pl
	case int64:
		info.PieceLength = int(pl)
	default:
		return fmt.Errorf("%s field missing or of incorrect type", _keyPieceLength)
	}
	return nil
}

// parsePiecesField parses the "pieces" field
func parsePiecesField(infoDict map[string]any, info *types.InfoDictionary) error {
	pieces, ok := infoDict[_keyPieces].(string)
	if !ok {
		return fmt.Errorf("%s field missing or not a string", _keyPieces)
	}
	info.Pieces = []byte(pieces)
	return nil
}

// parseLengthOrFiles parses the "length" or "files" field
func parseLengthOrFiles(infoDict map[string]any, info *types.InfoDictionary) error {
	if length, ok := infoDict[_keyLength].(int); ok {
		info.Length = int64(length)
	} else if length, ok := infoDict[_keyLength].(int64); ok {
		info.Length = length
	} else if files, ok := infoDict[_keyFiles].([]any); ok {
		parsedFiles, err := parseFiles(files)
		if err != nil {
			return fmt.Errorf("failed to parse files: %w", err)
		}
		info.Files = &parsedFiles
	} else {
		return fmt.Errorf("neither %s nor %s field found. Torrent may be missing fields", _keyLength, _keyFiles)
	}
	return nil
}

// parseFiles parses the "files" field into a list of File objects
func parseFiles(files []any) ([]types.File, error) {
	var fileList []types.File
	for _, file := range files {
		fileDict, ok := file.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("file entry is not a valid dictionary, got %T", file)
		}
		fileInfo, err := parseFile(fileDict)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file: %w", err)
		}
		fileList = append(fileList, fileInfo)
	}
	return fileList, nil
}

// parseFile parses a single file entry
func parseFile(fileDict map[string]any) (types.File, error) {
	var fileInfo types.File

	if length, ok := fileDict[_keyLength].(int); ok {
		fileInfo.Length = int64(length)
	} else if length, ok := fileDict[_keyLength].(int64); ok {
		fileInfo.Length = length
	} else {
		return fileInfo, fmt.Errorf("file length missing or not a valid type")
	}

	if path, ok := fileDict["path"].([]any); ok {
		for _, p := range path {
			if pStr, ok := p.(string); ok {
				fileInfo.Path = append(fileInfo.Path, pStr)
			} else {
				return fileInfo, fmt.Errorf("path element is not a valid type, got %T", p)
			}
		}
	} else {
		return fileInfo, fmt.Errorf("path missing or of incorrect type")
	}
	return fileInfo, nil
}
