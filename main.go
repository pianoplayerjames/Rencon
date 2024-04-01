package main

import (
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io/ioutil"
    "net"
    "os"
    "sync"
    "time"
)

type Node struct {
    ID   string
    Addr string
}

type Server struct {
    node        *Node
    onlineNodes map[string]*Node
    mu          sync.RWMutex
}

type Config struct {
    BootstrapServers []string `json:"bootstrap_servers"`
}

type Message struct {
    From    string `json:"from"`
    Content string `json:"content"`
}

func NewServer(addr string) *Server {
    node := &Node{
        ID:   generateNodeID(addr),
        Addr: addr,
    }

    server := &Server{
        node:        node,
        onlineNodes: make(map[string]*Node),
    }

    return server
}

func (s *Server) Start() error {
    listener, err := net.Listen("tcp", s.node.Addr)
    if err != nil {
        return err
    }
    defer listener.Close()

    fmt.Printf("Server started on %s\n", s.node.Addr)

    go s.pingNodes()

    for {
        conn, err := listener.Accept()
        if err != nil {
            fmt.Println("Error accepting connection:", err)
            continue
        }
        go s.handleConnection(conn)
    }
}

func (s *Server) handleConnection(conn net.Conn) {
    defer conn.Close()

    // Send the server's list of online nodes to the connected node
    err := s.sendOnlineNodes(conn)
    if err != nil {
        fmt.Println("Error sending node list:", err)
        return
    }

    // Receive the list of online nodes from the connected node
    onlineNodes, err := s.receiveOnlineNodes(conn)
    if err != nil {
        fmt.Println("Error receiving node list:", err)
        return
    }

    // Merge the received online nodes with the server's list
    s.mergeOnlineNodes(onlineNodes)

    // Handle incoming messages
    for {
        var message Message
        decoder := json.NewDecoder(conn)
        err := decoder.Decode(&message)
        if err != nil {
            fmt.Println("Error decoding message:", err)
            s.removeNode(generateNodeID(conn.RemoteAddr().String()))
            break
        }

        fmt.Printf("Received message from %s: %s\n", message.From, message.Content)
    }
}

func getIPAddress() (string, error) {
    addrs, err := net.InterfaceAddrs()
    if err != nil {
        return "", err
    }

    for _, addr := range addrs {
        if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
            if ipnet.IP.To4() != nil {
                return ipnet.IP.String(), nil
            }
        }
    }

    return "", fmt.Errorf("no valid IP address found")
}

func (s *Server) sendOnlineNodes(conn net.Conn) error {
    s.mu.RLock()
    defer s.mu.RUnlock()
    encoder := json.NewEncoder(conn)
    return encoder.Encode(s.onlineNodes)
}

func (s *Server) receiveOnlineNodes(conn net.Conn) (map[string]*Node, error) {
    decoder := json.NewDecoder(conn)
    var onlineNodes map[string]*Node
    err := decoder.Decode(&onlineNodes)
    return onlineNodes, err
}

func (s *Server) mergeOnlineNodes(receivedNodes map[string]*Node) {
    s.mu.Lock()
    defer s.mu.Unlock()
    for id, node := range receivedNodes {
        if _, exists := s.onlineNodes[id]; !exists && id != s.node.ID {
            s.onlineNodes[id] = node
            fmt.Printf("Merging node: %s\n", node.Addr)
        }
    }
}

func (s *Server) AddNode(node *Node) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.onlineNodes[node.ID] = node

    // Add the new node to the list of bootstrap servers
    updateConfigFile(node, true)
}

func (s *Server) GetConnectedNodes() []*Node {
    s.mu.RLock()
    defer s.mu.RUnlock()
    connectedNodes := make([]*Node, 0, len(s.onlineNodes))
    for _, node := range s.onlineNodes {
        connectedNodes = append(connectedNodes, node)
    }
    fmt.Printf("Currently aware of %d nodes.\n", len(connectedNodes))
    return connectedNodes
}

func (s *Server) pingNodes() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        configData, err := ioutil.ReadFile("config.json")
        if err != nil {
            fmt.Println("Error reading config file:", err)
            continue
        }

        var config Config
        err = json.Unmarshal(configData, &config)
        if err != nil {
            fmt.Println("Error parsing config data:", err)
            continue
        }

        for _, addr := range config.BootstrapServers {
            if addr == s.node.Addr {
                continue
            }

            nodeID := generateNodeID(addr)
            if _, exists := s.onlineNodes[nodeID]; exists {
                continue
            }

            conn, err := net.Dial("tcp", addr)
            if err != nil {
                fmt.Printf("Error connecting to node %s: %s\n", addr, err)
                s.removeNode(nodeID)
                continue
            }

            node := &Node{
                ID:   nodeID,
                Addr: addr,
            }

            s.AddNode(node)
            fmt.Printf("Connected to node %s\n", addr)

            conn.Close()
        }
    }
}

func (s *Server) removeNode(nodeID string) {
    s.mu.Lock()
    defer s.mu.Unlock()

    node, exists := s.onlineNodes[nodeID]
    if exists {
        delete(s.onlineNodes, nodeID)
        updateConfigFile(node, false)
        fmt.Printf("Removed node: %s\n", node.Addr)
    }
}

func (s *Server) sendMessage(addr string, message Message) error {
    conn, err := net.Dial("tcp", addr)
    if err != nil {
        return err
    }
    defer conn.Close()

    encoder := json.NewEncoder(conn)
    return encoder.Encode(message)
}

func generateNodeID(addr string) string {
    hash := sha256.Sum256([]byte(addr))
    return hex.EncodeToString(hash[:])
}

var config Config

func updateConfigFile(node *Node, add bool) {
    // Read the existing config file
    configData, err := ioutil.ReadFile("config.json")
    if err != nil {
        fmt.Println("Error reading config file:", err)
        return
    }

    // Parse the existing config data
    var existingConfig Config
    err = json.Unmarshal(configData, &existingConfig)
    if err != nil {
        fmt.Println("Error parsing existing config data:", err)
        return
    }

    // Check if the node exists in the list of bootstrap servers
    index := -1
    for i, addr := range existingConfig.BootstrapServers {
        if addr == node.Addr {
            index = i
            break
        }
    }

    if add {
        // Add the node to the list of bootstrap servers only if it doesn't already exist
        if index == -1 {
            existingConfig.BootstrapServers = append(existingConfig.BootstrapServers, node.Addr)
        }
    } else {
        // Remove the node from the list of bootstrap servers if it exists
        if index != -1 {
            existingConfig.BootstrapServers = append(existingConfig.BootstrapServers[:index], existingConfig.BootstrapServers[index+1:]...)
        }
    }

    // Marshal the updated config data
    updatedConfigData, err := json.MarshalIndent(existingConfig, "", "  ")
    if err != nil {
        fmt.Println("Error marshaling updated config data:", err)
        return
    }

    // Write the updated config data back to the file
    err = ioutil.WriteFile("config.json", updatedConfigData, 0644)
    if err != nil {
        fmt.Println("Error writing updated config file:", err)
    }
}

func main() {
    configData, err := ioutil.ReadFile("config.json")
    if err != nil {
        fmt.Println("Error reading config.json:", err)
        return
    }

    err = json.Unmarshal(configData, &config)
    if err != nil {
        fmt.Println("Error parsing config.json:", err)
        return
    }

    // Get the IP address of the machine
    ip, err := getIPAddress()
    if err != nil {
        fmt.Println("Error getting IP address:", err)
        return
    }

    port := 8001
    addr := fmt.Sprintf("%s:%d", ip, port)
    for {
        listener, err := net.Listen("tcp", addr)
        if err == nil {
            listener.Close()
            break
        }
        port++
        addr = fmt.Sprintf("%s:%d", ip, port)
    }

    server := NewServer(addr)
    go server.Start()

    // Connect to bootstrap nodes
    for _, addr := range config.BootstrapServers {
        if addr == server.node.Addr {
            continue
        }

        conn, err := net.Dial("tcp", addr)
        if err != nil {
            fmt.Printf("Error connecting to bootstrap node %s: %s\n", addr, err)
            continue
        }

        node := &Node{
            ID:   generateNodeID(addr),
            Addr: addr,
        }

        server.AddNode(node)
        fmt.Printf("Connected to bootstrap node %s\n", addr)

        time.Sleep(time.Second)
        conn.Close()
    }

    // Add the current node to the list of bootstrap servers
    currentNode := &Node{
        ID:   server.node.ID,
        Addr: server.node.Addr,
    }
    server.AddNode(currentNode)

    go func() {
        for {
            fmt.Print("> ")
            var command string
            fmt.Scanln(&command)

            if command == "online" {
                connectedNodes := server.GetConnectedNodes()
                if len(connectedNodes) == 0 {
                    fmt.Println("No connected nodes found.")
                } else {
                    fmt.Println("Connected Nodes:")
                    for _, node := range connectedNodes {
                        fmt.Printf("ID: %s, Address: %s\n", node.ID, node.Addr)
                    }
                }
            } else if command == "send" {
                fmt.Print("Enter destination node address: ")
                var destAddr string
                fmt.Scanln(&destAddr)

                fmt.Print("Enter message content: ")
                var content string
                fmt.Scanln(&content)

                message := Message{
                    From:    server.node.Addr,
                    Content: content,
                }

                err := server.sendMessage(destAddr, message)
                if err != nil {
                    fmt.Printf("Error sending message to %s: %s\n", destAddr, err)
                    server.removeNode(generateNodeID(destAddr))
                } else {
                    fmt.Printf("Message sent to %s\n", destAddr)
                }
            } else if command == "exit" {
                fmt.Println("Exiting...")
                os.Exit(0)
            } else {
                fmt.Println("Unknown command. Available commands: online, send, exit")
            }
        }
    }()

    select {}
}