package main

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "os"

    "github.com/libp2p/go-libp2p"
    "github.com/libp2p/go-libp2p/core/crypto"
    "github.com/libp2p/go-libp2p/core/host"
    "github.com/libp2p/go-libp2p/core/network"
    "github.com/libp2p/go-libp2p/core/peer"
    "github.com/libp2p/go-libp2p/core/protocol"
    "github.com/libp2p/go-libp2p-kad-dht"
    "github.com/multiformats/go-multiaddr"
)

type Config struct {
    BootstrapPeers []string `json:"bootstrapPeers"`
    ListenAddress  string   `json:"listenAddress"`
    ProtocolID     string   `json:"protocolID"`
}

func main() {
    // Load configuration from file
    configFile, err := os.Open("config.json")
    if err != nil {
        panic(err)
    }
    defer configFile.Close()

    var config Config
    if err := json.NewDecoder(configFile).Decode(&config); err != nil {
        panic(err)
    }

    // Generate a new libp2p host with a static key
    privateKey, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
    if err != nil {
        panic(err)
    }

    privateKeyBytes, err := crypto.MarshalPrivateKey(privateKey)
    if err != nil {
        panic(err)
    }

	privateKeyPEM := crypto.ConfigEncodeKey(privateKeyBytes)

	// Save the PEM key to a file in the root directory
	pemFile, err := os.Create("node_key.pem")
	if err != nil {
		panic(err)
	}
	defer pemFile.Close()
	
	if _, err := pemFile.Write([]byte(privateKeyPEM)); err != nil {
		panic(err)
	}

    host, err := libp2p.New(
        libp2p.ListenAddrStrings(config.ListenAddress),
        libp2p.Identity(privateKey),
    )
    if err != nil {
        panic(err)
    }

    // Connect to bootstrap peers
    for _, peerAddr := range config.BootstrapPeers {
        addr, err := multiaddr.NewMultiaddr(peerAddr)
        if err != nil {
            fmt.Printf("Failed to parse bootstrap peer address: %s\n", err)
            continue
        }

        peerInfo, err := peer.AddrInfoFromP2pAddr(addr)
        if err != nil {
            fmt.Printf("Failed to get peer info from address: %s\n", err)
            continue
        }

        if err := host.Connect(context.Background(), *peerInfo); err != nil {
            fmt.Printf("Failed to connect to bootstrap peer: %s\n", err)
            continue
        }

        fmt.Printf("Connected to bootstrap peer: %s\n", peerInfo.ID)
    }

    // Create a new DHT
    dht, err := dht.New(context.Background(), host)
    if err != nil {
        panic(err)
    }

    // Set up a stream handler for the protocol
    host.SetStreamHandler(protocol.ID(config.ProtocolID), handleStream)

    // Start a terminal interface
    go terminal(host, dht)

    // Broadcast our own address to the network
    broadcastAddress(host, config.ProtocolID)

    // Wait for termination
    select {}
}

func handleStream(stream network.Stream) {
    // Handle incoming stream connections
    // Implement your custom stream handling logic here
    fmt.Printf("Received a new stream from %s\n", stream.Conn().RemotePeer())
    // Read and write messages to the stream
    // ...
    stream.Close()
}

func terminal(host host.Host, dht *dht.IpfsDHT) {
    reader := bufio.NewReader(os.Stdin)
    for {
        fmt.Print("> ")
        command, _ := reader.ReadString('\n')
        command = command[:len(command)-1] // Remove the newline character

        switch command {
        case "online":
            printConnectedPeers(host)
        default:
            fmt.Println("Unknown command")
        }
    }
}

func printConnectedPeers(host host.Host) {
    fmt.Println("Connected peers:")
    for _, peerID := range host.Network().Peers() {
        fmt.Printf("- %s\n", peerID)
    }
}

func broadcastAddress(host host.Host, protocolID string) {
    for _, peerID := range host.Network().Peers() {
        if peerID == host.ID() {
            continue // Skip broadcasting to self
        }

        stream, err := host.NewStream(context.Background(), peerID, protocol.ID(protocolID))
        if err != nil {
            fmt.Printf("Failed to open stream to peer %s: %s\n", peerID, err)
            continue
        }
        defer stream.Close()

        // Send our own address to the peer
        ourAddress := host.Addrs()[0].String() + "/p2p/" + host.ID().String()
        if _, err := stream.Write([]byte(ourAddress)); err != nil {
            fmt.Printf("Failed to send address to peer %s: %s\n", peerID, err)
        }
    }
}