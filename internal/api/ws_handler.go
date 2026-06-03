package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"dashbox/internal/db"
	"dashbox/internal/ws"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (r *Router) wsHandler(c *gin.Context) {
	serial := c.Query("serial")
	dongleID := c.Query("dongle_id")
	if serial == "" || dongleID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing serial or dongle_id"})
		return
	}

	dev, err := db.GetDeviceBySerial(r.db, serial)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "device not registered"})
		return
	}
	if dev.DeviceID != dongleID {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "dongle_id mismatch"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ws] Upgrade error: %v", err)
		return
	}

	// Heartbeat: server pings every 15s; if no pong within 35s, connection dies
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(35 * time.Second))
		db.UpdateDeviceOnline(r.db, dev.DeviceID, true)
		return nil
	})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				return
			}
		}
	}()

	client := &ws.Client{
		DeviceID: dev.DeviceID,
		Conn:     conn,
		Send:     make(chan []byte, 64),
	}

	r.hub.Register(dev.DeviceID, client)
	db.UpdateDeviceOnline(r.db, dev.DeviceID, true)

	clientIP := c.ClientIP()
	netType := "cellular"
	if strings.HasPrefix(clientIP, "10.") || strings.HasPrefix(clientIP, "192.168.") || strings.HasPrefix(clientIP, "172.") {
		netType = "wifi"
	}
	db.UpdateDeviceNetwork(r.db, dev.DeviceID, netType)

	go r.wsWriter(client)
	r.wsReader(client, dev.DeviceID)
}

func (r *Router) wsAppHandler(c *gin.Context) {
	token := c.Query("token")
	serial := c.Query("serial")
	dongleID := c.Query("dongle_id")

	if token == "" || serial == "" || dongleID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing token, serial, or dongle_id"})
		return
	}

	user, err := db.GetUserByToken(r.db, token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	dev, err := db.GetDeviceBySerial(r.db, serial)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "device not found"})
		return
	}

	if dev.DeviceID != dongleID {
		c.JSON(http.StatusForbidden, gin.H{"error": "dongle_id mismatch"})
		return
	}

	if dev.UserID == nil || *dev.UserID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "not bound to this device"})
		return
	}

	log.Printf("[ws/app] user %d connected to device %s", user.ID, dev.DeviceID)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ws/app] Upgrade error: %v", err)
		return
	}

	client := &ws.Client{
		DeviceID: dev.DeviceID,
		Conn:     conn,
		Send:     make(chan []byte, 64),
	}

	r.hub.RegisterApp(dev.DeviceID, client)
	defer r.hub.UnregisterApp(dev.DeviceID, client)

	go r.wsWriter(client)

	for {
		client.Conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, _, err := client.Conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (r *Router) wsReader(client *ws.Client, deviceID string) {
	defer func() {
		r.hub.Unregister(deviceID, client)
		db.UpdateDeviceOnline(r.db, deviceID, false)
		client.Conn.Close()
	}()

	for {
		client.Conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, msgBytes, err := client.Conn.ReadMessage()
		if err != nil {
			break
		}

		var resp ws.RPCResponse
		if err := json.Unmarshal(msgBytes, &resp); err == nil && (resp.Result != nil || resp.Error != nil) {
			r.hub.HandleResponse(&resp)
			continue
		}

		var req ws.RPCRequest
		if err := json.Unmarshal(msgBytes, &req); err != nil || req.Method == "" {
			continue
		}

		r.handleRPCMessage(client, deviceID, &req)
	}
}

func (r *Router) wsWriter(client *ws.Client) {
	for msg := range client.Send {
		if err := client.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
}

func (r *Router) handleRPCMessage(client *ws.Client, deviceID string, req *ws.RPCRequest) {
	switch req.Method {
	case "vehicle_info":
		var data struct {
			Brand   string `json:"brand"`
			Model   string `json:"model"`
			Version string `json:"version"`
			Branch  string `json:"branch"`
		}
		if err := json.Unmarshal(req.Params, &data); err == nil {
			db.UpdateDeviceVehicle(r.db, deviceID, data.Brand, data.Model, data.Version, data.Branch)
		}
		r.wsReply(client, req.ID, "ok")

	case "params_sync":
		var data struct {
			Params map[string]string `json:"params"`
		}
		if err := json.Unmarshal(req.Params, &data); err == nil && len(data.Params) > 0 {
			log.Printf("[ws] params_sync: %d params from %s", len(data.Params), deviceID)
			// Forward to all connected App clients as params_update
			updateMsg, _ := json.Marshal(map[string]interface{}{
				"method": "params_update",
				"params": data.Params,
			})
			r.hub.NotifyApp(deviceID, updateMsg)
		}
		r.wsReply(client, req.ID, "ok")

	case "ping":
		r.wsReply(client, req.ID, "pong")

	case "generate_pairing_code":
		code, err := db.GeneratePairingCode(r.db, deviceID)
		if err != nil {
			r.wsError(client, req.ID, -32603, err.Error())
			return
		}
		log.Printf("[ws] pairing code %s generated for %s via WS", code, deviceID)
		r.wsReply(client, req.ID, gin.H{"pairing_code": code, "expires_in": 900})

	case "getParams":
		params, err := db.GetDeviceParams(r.db, deviceID)
		if err != nil {
			r.wsError(client, req.ID, -32603, err.Error())
			return
		}
		r.wsReply(client, req.ID, gin.H{"params": params})

	case "saveParams":
		var data struct {
			Params map[string]string `json:"params"`
		}
		if err := json.Unmarshal(req.Params, &data); err != nil {
			r.wsError(client, req.ID, -32602, "invalid params")
			return
		}
		if err := db.SaveDeviceParams(r.db, deviceID, data.Params); err != nil {
			r.wsError(client, req.ID, -32603, err.Error())
			return
		}
		r.wsReply(client, req.ID, "ok")

	default:
		r.wsError(client, req.ID, -32601, "method not found")
	}
}

func (r *Router) wsReply(client *ws.Client, id interface{}, result interface{}) {
	resp := ws.RPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, _ := json.Marshal(resp)
	client.Send <- data
}

func (r *Router) wsError(client *ws.Client, id interface{}, code int, message string) {
	resp := ws.RPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &ws.RPCError{Code: code, Message: message},
	}
	data, _ := json.Marshal(resp)
	client.Send <- data
}
