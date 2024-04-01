package main

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"fmt"
	"os"
	"time"

	"github.com/libp2p/go-libp2p"
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

    // 's' will be closed when this function exits. To keep the stream open, we'll hang the function.
    select {}
}

func readData(rw *bufio.ReadWriter) {
    for {
        str, _ := rw.ReadString('\n')
        if str == "" {
            return
        }
        if str != "\n" {
            fmt.Printf("Received: %s", str)
        }
    }
}

func writeData(rw *bufio.ReadWriter) {
    for {
        fmt.Print("> ")
        sendData, err := bufio.NewReader(os.Stdin).ReadString('\n')
        if err != nil {
            fmt.Println("Error reading from stdin:", err)
            continue
        }

        rw.WriteString(sendData)
        rw.Flush()
    }
}



func startNode(config *Config) (host.Host, *dht.IpfsDHT) {
	// Create a libp2p node with the specified listen address
	node, err := libp2p.New(libp2p.ListenAddrStrings(config.ListenAddress))
	if err != nil {
		panic(err)
	}

	// Create a new DHT
	dht, err := dht.New(context.Background(), node)
	if err != nil {
		panic(err)
	}

	// Set the stream handler
	node.SetStreamHandler(protocol.ID(config.ProtocolID), handleStream)

	return node, dht
}

func connectToVPS(ctx context.Context, node host.Host, vpsAddr string) error {
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

    fmt.Printf("Connected to VPS: %s\n", vpsAddr)
    return nil
}

func discoverPeers(ctx context.Context, node host.Host, dht *dht.IpfsDHT) {
	for {
		// Find closest peers to the node's ID
		peers, err := dht.GetClosestPeers(ctx, string(node.ID()))
		if err != nil {
			fmt.Println("Error discovering peers:", err)
			return
		}

		for _, p := range peers {
			if p == node.ID() {
				continue // Skip self
			}
			fmt.Printf("Discovered peer: %s\n", p)
			node.Connect(ctx, peer.AddrInfo{ID: p})
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

	node, dht := startNode(config)
	fmt.Println("Your peer ID:", node.ID())
	fmt.Println("Listening on:")
	for _, addr := range node.Addrs() {
		fmt.Printf("%s/p2p/%s\n", addr, node.ID())
	}

	// Set up a handler for new peer connections
	node.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(network network.Network, conn network.Conn) {
			fmt.Printf("New peer connected: %s\n", conn.RemotePeer())
		},
	})

	// Connect to all bootstrap peers
	for _, vpsAddr := range config.BootstrapPeers {
		if err := connectToVPS(ctx, node, vpsAddr); err != nil {
			fmt.Println("Failed to connect to VPS:", err)
			continue
		}
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
			cancel() // Cancel the context to gracefully shutdown
			return
		} else {
			fmt.Println("Unknown command. Available commands:")
			fmt.Println("  online - Print a list of connected peers")
			fmt.Println("  exit   - Exit the program")
		}
	}
}