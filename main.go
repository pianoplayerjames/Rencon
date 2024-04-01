package main

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "os"

    "github.com/libp2p/go-libp2p"
    "github.com/libp2p/go-libp2p/core/host"
    "github.com/libp2p/go-libp2p/core/network"
    "github.com/libp2p/go-libp2p/core/peer"
    "github.com/libp2p/go-libp2p/core/protocol"
    "github.com/multiformats/go-multiaddr"
    "github.com/libp2p/go-libp2p/core/crypto"
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

func startNode(config *Config) host.Host {
    // Generate a new key pair
    privateKey, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
    if err != nil {
        panic(err)
    }

    // Create a libp2p node with the generated key pair
    node, err := libp2p.New(
        libp2p.ListenAddrStrings(config.ListenAddress),
        libp2p.Identity(privateKey),
    )
    if err != nil {
        panic(err)
    }

    // Set the stream handler
    node.SetStreamHandler(protocol.ID(config.ProtocolID), handleStream)

    return node
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

    fmt.Println("Connected to VPS:", vpsAddr)
    return nil
}

func main() {
    config, err := loadConfig("config.json")
    if err != nil {
        fmt.Println("Failed to load configuration:", err)
        return
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    node := startNode(config)
    fmt.Println("Your peer ID:", node.ID())
    fmt.Println("Listening on:")
    for _, addr := range node.Addrs() {
        fmt.Printf("%s/p2p/%s\n", addr, node.ID())
    }

    // Connect to all bootstrap peers
    for _, vpsAddr := range config.BootstrapPeers {
        if err := connectToVPS(ctx, node, vpsAddr); err != nil {
            fmt.Println("Failed to connect to VPS:", err)
            continue
        }
    }

    // Broadcast own node to the network
    for _, peerAddr := range node.Addrs() {
        fmt.Printf("Broadcasting own node: %s/p2p/%s\n", peerAddr, node.ID())
    }

    // Hang forever.
    <-make(chan struct{})
}