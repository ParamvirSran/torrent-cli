package peers

import (
	"fmt"
	"net"
)

// ExtractPeers will take the peers returned from a tracker and return the parsed peer list
func ExtractPeers(trackerResp map[string]any) ([]string, []string, error) {
	var (
		peerList     []string
		peer_id_list []string
		err          error
	)
	if peers, ok := trackerResp["peers"].(string); ok {
		peerList, err = parseCompactPeers([]byte(peers))
	} else if peers, ok := trackerResp["peers"].([]any); ok {
		peer_id_list, peerList, err = parseDictionaryPeers(peers)
	}
	return peer_id_list, peerList, err
}

// parseDictionaryPeers will return the peerlist when trackers provide us a standard peerlist in map format
func parseDictionaryPeers(peers []any) ([]string, []string, error) {
	var (
		peer_id_list []string
		peerList     []string
	)
	for _, peer := range peers {
		if peerMap, ok := peer.(map[string]any); ok {
			ip, ipOk := peerMap["ip"].(string)
			port, portOk := peerMap["port"].(int)
			peerID, idOk := peerMap["peer id"].(string)

			if ipOk && portOk && idOk {
				peerList = append(peerList, fmt.Sprintf("%s:%d", ip, port))
				peer_id_list = append(peer_id_list, peerID)
			} else {
				return nil, nil, fmt.Errorf("peer ip: %t, peer port: %t, peer id: %t", ipOk, portOk, idOk)
			}
		} else {
			return nil, nil, fmt.Errorf("invalid peer format, expecting dictionary format from tracker")
		}
	}
	return peer_id_list, peerList, nil
}

// parseCompactPeers will parse the compact peer list when we get that format
func parseCompactPeers(peers []byte) ([]string, error) {
	var peerList []string

	if len(peers)%6 != 0 {
		return nil, fmt.Errorf("invalid compact peers length")
	}

	for i := 0; i < len(peers); i += 6 {
		ip := net.IP(peers[i : i+4]).String()
		port := int(peers[i+4])<<8 + int(peers[i+5])
		peer := fmt.Sprintf("%s:%d", ip, port)
		peerList = append(peerList, peer)
	}
	return peerList, nil
}
