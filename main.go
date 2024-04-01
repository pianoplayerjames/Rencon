package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

type Config struct {
	BootstrapPeers []string `json:"bootstrapPeers"`
	ListenAddress  string   `json:"listenAddress"`
	ProtocolID     string   `json:"protocolID"`
}

const (
	peerListRequestMessage  = "peer_list_request"
	peerListResponseMessage = "peer_list_response"
	keyFilePath             = "bootstrap_key.pem"
)

func loadConfig(filePath string) (*Config, error) {
    file, err := os.Open(filePath)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    var config Config
    decoder := json.NewDecoder(file)
    if err := decoder.Decode(&config); err != nil {
        return nil, err
    }

    return &config, nil
}

func handleStream(s network.Stream) {
    fmt.Println("Got a new stream!")

    // Create a buffer stream for non blocking read and write.
    rw := bufio.NewReadWriter(bufio.NewReader(s), bufio.NewWriter(s))

    go readData(rw)
    go writeData(rw)

    // Send peer list to the new node
    if err := sendPeerList(s, s.Conn().RemotePeer()); err != nil {
        fmt.Println("Error sending peer list:", err)
        return
    }

}

func readData(rw *bufio.ReadWriter) {
    for {
        str, err := rw.ReadString('\n')
        if err != nil {
            fmt.Println("Error reading from buffer:", err)
            return
        }

        if str == "" {
            return
        }
        if str != "\n" {
            // Green console colour:     \x1b[32m
            // Reset console colour:     \x1b[0m
            fmt.Printf("\x1b[32m%s\x1b[0m> ", str)
        }
    }
}

func writeData(rw *bufio.ReadWriter) {
    stdReader := bufio.NewReader(os.Stdin)

    for {
        fmt.Print("> ")
        sendData, err := stdReader.ReadString('\n')
        if err != nil {
            fmt.Println("Error reading from stdin:", err)
            return
        }

        _, err = rw.WriteString(sendData)
        if err != nil {
            fmt.Println("Error writing to buffer:", err)
            return
        }
        err = rw.Flush()
        if err != nil {
            fmt.Println("Error flushing buffer:", err)
            return
        }
    }
}

func sendPeerList(s network.Stream, p peer.ID) error {
    // Get the list of connected peers
    peers := s.Conn().RemoteMultiaddr().Bytes()

    // Marshal the list of peers to JSON
    peerListJSON, err := json.Marshal(peers)
    if err != nil {
        return err
    }

    // Send the peer list to the stream
    _, err = s.Write(append([]byte(peerListResponseMessage), peerListJSON...))
    return err
}

func receivePeerList(s network.Stream) ([]peer.ID, error) {
    // Read the message from the stream
    message, err := bufio.NewReader(s).ReadString('\n')
    if err != nil {
        return nil, err
    }

    // Check if the message is a peer list response
    if strings.HasPrefix(message, peerListResponseMessage) {
        // Extract the peer list JSON from the message
        peerListJSON := strings.TrimPrefix(message, peerListResponseMessage)

        // Unmarshal the peer list JSON
        var peerList []byte
        if err := json.Unmarshal([]byte(peerListJSON), &peerList); err != nil {
            return nil, err
        }

        // Convert the peer list to peer.ID format
        var peers []peer.ID
        for _, p := range peerList {
            id, err := peer.IDFromBytes([]byte{p})
            if err != nil {
                continue
            }
            peers = append(peers, id)
        }

        return peers, nil
    }

    return nil, fmt.Errorf("unexpected message received: %s", message)
}

// generatePersistentKeyPair generates a new key pair and saves it to a file.
func generatePersistentKeyPair(filepath string) error {
	privKey, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
	if err != nil {
		return err
	}

	privKeyBytes, err := crypto.MarshalPrivateKey(privKey)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filepath, privKeyBytes, 0600)
	if err != nil {
		return err
	}

	return nil
}

// loadPersistentKeyPair loads a previously generated key pair from a file.
func loadPersistentKeyPair(filepath string) (crypto.PrivKey, error) {
	privKeyBytes, err := ioutil.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	privKey, err := crypto.UnmarshalPrivateKey(privKeyBytes)
	if err != nil {
		return nil, err
	}

	return privKey, nil
}

func startNode(ctx context.Context, config *Config) (host.Host, *dht.IpfsDHT, error) {
	fmt.Println("Starting node...")

	// Load the persistent key pair
	privKey, err := loadPersistentKeyPair(keyFilePath)
	if err != nil {
		// If the key pair doesn't exist, generate a new one and save it
		err := generatePersistentKeyPair(keyFilePath)
		if err != nil {
			return nil, nil, err
		}
		privKey, err = loadPersistentKeyPair(keyFilePath)
		if err != nil {
			return nil, nil, err
		}
	}

	// Create a new libp2p host with the loaded key pair
	host, err := libp2p.New(libp2p.Identity(privKey), libp2p.ListenAddrStrings(config.ListenAddress))
	if err != nil {
		return nil, nil, err
	}

	// Create a new DHT
	dht, err := dht.New(ctx, host)
	if err != nil {
		return nil, nil, err
	}

	// Set the stream handler on the host
	host.SetStreamHandler(protocol.ID(config.ProtocolID), handleStream)

	fmt.Println("Node started with ID:", host.ID())

	// Set up periodic peer list updates
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := updatePeerList(ctx, host, config); err != nil {
					fmt.Println("Error updating peer list:", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return host, dht, nil
}

func connectToVPS(ctx context.Context, node host.Host, vpsAddr string) error {
    fmt.Println("Connecting to VPS:", vpsAddr)

    addr, err := multiaddr.NewMultiaddr(vpsAddr)
    if err != nil {
        return err
    }

    peerInfo, err := peer.AddrInfoFromP2pAddr(addr)
    if err != nil {
        return err
    }

    if err := node.Connect(ctx, *peerInfo); err != nil {
        return err
    }

    fmt.Println("Connected to VPS:", vpsAddr)

    return nil
}

func updatePeerList(ctx context.Context, node host.Host, config *Config) error {
    // Connect to the bootstrap node
    for _, vpsAddr := range config.BootstrapPeers {
        if err := connectToVPS(ctx, node, vpsAddr); err != nil {
            fmt.Println("Failed to connect to VPS:", err)
            continue
        }

        // Send a peer list request to the bootstrap node
        s, err := node.NewStream(ctx, peer.ID(vpsAddr), protocol.ID(config.ProtocolID))
        if err != nil {
            fmt.Println("Error creating stream to bootstrap node:", err)
            continue
        }

        if err := sendPeerListRequest(s); err != nil {
            fmt.Println("Error sending peer list request:", err)
            s.Close()
            continue
        }

        // Receive the peer list response from the bootstrap node
        peers, err := receivePeerList(s)
        if err != nil {
            fmt.Println("Error receiving peer list:", err)
            s.Close()
            continue
        }

        // Update the node's peer list
        for _, p := range peers {
            node.Peerstore().AddAddrs(p, node.Peerstore().Addrs(p), time.Hour)
        }

        s.Close()
    }

    return nil
}

func sendPeerListRequest(s network.Stream) error {
    _, err := s.Write([]byte(peerListRequestMessage + "\n"))
    return err
}

func discoverPeers(ctx context.Context, node host.Host, dht *dht.IpfsDHT) {
    fmt.Println("Starting peer discovery...")

    for {
        // Find closest peers to the node's ID
        peers, err := dht.GetClosestPeers(ctx, string(node.ID()))
        if err != nil {
            fmt.Println("Error discovering peers:", err)
            return
        }

        fmt.Println("Discovered peers:")
        for _, p := range peers {
            if p == node.ID() {
                continue // Skip self
            }
            fmt.Printf("  - %s\n", p)

            // Connect to the discovered peer
            fmt.Println("Connecting to peer:", p)
            if err := node.Connect(ctx, peer.AddrInfo{ID: p}); err != nil {
                fmt.Println("Error connecting to peer:", err)
                continue
            }
            fmt.Println("Connected to peer:", p)
        }

        select {
        case <-ctx.Done():
            return
        case <-time.After(time.Minute):
            continue
        }
    }
}

func printConnectedPeers(node host.Host) {
    fmt.Println("Connected peers:")
    for _, conn := range node.Network().Conns() {
        fmt.Printf("  - %s\n", conn.RemotePeer())
    }
}

func main() {
	config, err := loadConfig("config.json")
	if err != nil {
		fmt.Println("Failed to load configuration:", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node, dht, err := startNode(ctx, config)
	if err != nil {
		fmt.Println("Failed to start node:", err)
		return
	}
	fmt.Println("Node started with ID:", node.ID())
	fmt.Println("Listening on:")
	for _, addr := range node.Addrs() {
		fmt.Printf("  - %s/p2p/%s\n", addr, node.ID())
	}

	// Set up a handler for new peer connections
	node.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(network network.Network, conn network.Conn) {
			fmt.Printf("New peer connected: %s\n", conn.RemotePeer())
		},
	})

	// Connect to the bootstrap node and retrieve the initial peer list
	if err := updatePeerList(ctx, node, config); err != nil {
		fmt.Println("Failed to update peer list:", err)
	}

	// Start peer discovery
	go discoverPeers(ctx, node, dht)

	// Read user input from the terminal
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading input:", err)
			continue
		}
		input = strings.TrimSpace(input)

		if input == "online" {
			printConnectedPeers(node)
		} else if input == "exit" {
			fmt.Println("Exiting...")
			cancel()
			return
		} else {
			fmt.Println("Unknown command. Available commands:")
			fmt.Println("  online - Print a list of connected peers")
			fmt.Println("  exit   - Exit the program")
		}
	}
}