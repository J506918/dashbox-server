package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

// Hub manages WebSocket connections from devices and app clients.
type Hub struct {
	db           *gorm.DB
	mu           sync.RWMutex
	devices      map[string]*Client              // deviceID -> client
	appClients   map[string]map[*Client]struct{} // deviceID -> set of app clients
	pending      map[string]chan *RPCResponse
	reqIDCounter atomic.Int64
}

type Client struct {
	DeviceID string
	Conn     *websocket.Conn
	Send     chan []byte
	Stale    bool // marked when replaced by a newer connection
}

// JSON-RPC messages
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      interface{}     `json:"id,omitempty"`
}

type RPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewHub(db *gorm.DB) *Hub {
	return &Hub{
		db:        db,
		devices:   make(map[string]*Client),
		appClients: make(map[string]map[*Client]struct{}),
		pending:   make(map[string]chan *RPCResponse),
	}
}

func (h *Hub) Run() {
	// Hub lifecycle — cleanup stale connections etc.
	log.Println("[ws] Hub running")
	select {} // keep alive
}

func (h *Hub) Register(deviceID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Close any existing connection
	if old, ok := h.devices[deviceID]; ok {
		old.Stale = true
		close(old.Send)
		old.Conn.Close()
	}
	h.devices[deviceID] = client
	log.Printf("[ws] Device connected: %s (total: %d)", deviceID, len(h.devices))
}

func (h *Hub) Unregister(deviceID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existing, ok := h.devices[deviceID]; ok && existing == client {
		close(existing.Send)
		existing.Conn.Close()
		delete(h.devices, deviceID)
		log.Printf("[ws] Device disconnected: %s (total: %d)", deviceID, len(h.devices))
	}
}

// ── App client management ───────────────────────────────

func (h *Hub) RegisterApp(deviceID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.appClients[deviceID] == nil {
		h.appClients[deviceID] = make(map[*Client]struct{})
	}
	h.appClients[deviceID][client] = struct{}{}
	log.Printf("[ws] App connected: device=%s (total app conns: %d)", deviceID, len(h.appClients[deviceID]))
}

func (h *Hub) UnregisterApp(deviceID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.appClients[deviceID] != nil {
		delete(h.appClients[deviceID], client)
		if len(h.appClients[deviceID]) == 0 {
			delete(h.appClients, deviceID)
		}
	}
	client.Conn.Close()
	log.Printf("[ws] App disconnected: device=%s", deviceID)
}

// NotifyApp pushes a notification to all app clients of a user.
func (h *Hub) NotifyApp(deviceID string, msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients := h.appClients[deviceID]
	for c := range clients {
		select {
		case c.Send <- msg:
		default:
		}
	}
}

// ── RPC ─────────────────────────────────────────────────

func (h *Hub) SendRPC(deviceID string, req *RPCRequest) (*RPCResponse, error) {
	h.mu.RLock()
	client, ok := h.devices[deviceID]
	h.mu.RUnlock()

	if !ok {
		return nil, ErrDeviceOffline
	}

	// Always assign a unique ID — prevents concurrent requests from clobbering each other
	req.ID = h.reqIDCounter.Add(1)

	// Create a response channel for this request
	respChan := make(chan *RPCResponse, 1)
	idKey := fmt.Sprintf("%v", req.ID)
	h.mu.Lock()
	h.pending[idKey] = respChan
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, idKey)
		h.mu.Unlock()
	}()

	data, _ := json.Marshal(req)
	select {
	case client.Send <- data:
		log.Printf("[rpc] %s → device %s queued (%d bytes)", req.Method, deviceID, len(data))
	default:
		return nil, ErrSendBufferFull
	}

	// Wait for response with timeout
	select {
	case resp := <-respChan:
		log.Printf("[rpc] %s ← device %s response", req.Method, deviceID)
		return resp, nil
	case <-time.After(30 * time.Second):
		log.Printf("[rpc] %s → device %s TIMEOUT (30s, no response)", req.Method, deviceID)
		return nil, errors.New("rpc timeout")
	}
}

// HandleResponse routes an incoming RPC response to the waiting caller.
func (h *Hub) HandleResponse(resp *RPCResponse) {
	h.mu.RLock()
	idKey := fmt.Sprintf("%v", resp.ID)
	ch, ok := h.pending[idKey]
	h.mu.RUnlock()
	if ok {
		select {
		case ch <- resp:
		default:
		}
	}
}

var (
	ErrDeviceOffline  = errors.New("device offline")
	ErrSendBufferFull = errors.New("send buffer full")
)

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
