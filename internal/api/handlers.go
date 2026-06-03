package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"dashbox/internal/db"
	"dashbox/internal/models"
	"dashbox/internal/ws"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ── Device Registration ─────────────────────────────────────────

type RegisterDeviceRequest struct {
	Serial   string `json:"serial" `
	DongleID string `json:"dongle_id" `
}

func (r *Router) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (r *Router) registerDevice(c *gin.Context) {
	var req RegisterDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing, err := db.GetDeviceBySerial(r.db, req.Serial)
	if err == nil {
		c.JSON(http.StatusOK, gin.H{
			"device_id": existing.DeviceID,
			"dongle_id": existing.DeviceID,
			"serial":    existing.Serial,
		})
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	dev, err := db.CreateDevice(r.db, req.Serial)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register device"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"device_id": dev.DeviceID,
		"dongle_id": dev.DeviceID,
		"serial":    dev.Serial,
	})
}

// ── Device List ──────────────────────────────────────────────────

func (r *Router) listDevices(c *gin.Context) {
	userID, _ := c.Get("user_id")
	devs, err := db.ListDevicesByUser(r.db, userID.(int64))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if devs == nil {
		devs = []models.Device{}
	}
	c.JSON(http.StatusOK, gin.H{"devices": devs})
}

func (r *Router) getDevice(c *gin.Context) {
	dev, err := db.GetDeviceByID(r.db, c.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, dev)
}

func (r *Router) deviceStatus(c *gin.Context) {
	dev, err := db.GetDeviceByID(r.db, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"online": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"online": dev.Online, "last_seen": dev.LastSeen})
}

// ── Vehicle Info ─────────────────────────────────────────────────

type UpdateVehicleRequest struct {
	Brand   string `json:"brand"`
	Model   string `json:"model"`
	Version string `json:"version"`
	Branch  string `json:"branch"`
}

func (r *Router) updateVehicleInfo(c *gin.Context) {
	var req UpdateVehicleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := db.UpdateDeviceVehicle(r.db, c.Param("id"), req.Brand, req.Model, req.Version, req.Branch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ── Device Params ───────────────────────────────────────────────

func (r *Router) getDeviceParams(c *gin.Context) {
	params, err := db.GetDeviceParams(r.db, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"params": params})
}

type SaveParamsRequest struct {
	Params map[string]string `json:"params" `
}

func (r *Router) saveDeviceParams(c *gin.Context) {
	var req SaveParamsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := db.SaveDeviceParams(r.db, c.Param("id"), req.Params); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}


	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ── Drive Routes ─────────────────────────────────────────────────

type CreateDriveRouteRequest struct {
	StartTime string  `json:"start_time"`
	EndTime   string  `json:"end_time"`
	Distance  float64 `json:"distance"`
	Duration  int64   `json:"duration"`
	RouteData string  `json:"route_data"`
}

func (r *Router) createDriveRoute(c *gin.Context) {
	var req CreateDriveRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	startTime, _ := time.Parse(time.RFC3339, req.StartTime)
	endTime, _ := time.Parse(time.RFC3339, req.EndTime)

	err := db.CreateRoute(r.db, c.Param("id"), startTime, endTime, req.Distance, int(req.Duration), req.RouteData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}

func (r *Router) getDriveRoutes(c *gin.Context) {
	routes, err := db.GetDeviceRoutes(r.db, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if routes == nil {
		routes = []models.DriveRoute{}
	}
	c.JSON(http.StatusOK, gin.H{"routes": routes})
}

// ── Backups ──────────────────────────────────────────────────────

type CreateBackupRequest struct {
	Data string `json:"data" `
	Size int64  `json:"size"`
}

func (r *Router) createBackup(c *gin.Context) {
	var req CreateBackupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	backup, err := db.CreateBackup(r.db, c.Param("id"), req.Data, int(req.Size))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, backup)
}

func (r *Router) getBackup(c *gin.Context) {
	var req struct {
		Version int `json:"version"`
	}
	// support query param or body
	if v := c.Query("version"); v != "" {
		// simple parse
		if n := 0; true {
			for _, c := range v {
				if c >= '0' && c <= '9' {
					n = n*10 + int(c-'0')
				}
			}
			req.Version = n
		}
	}
	if req.Version == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "version required"})
		return
	}

	backup, err := db.GetBackup(r.db, c.Param("id"), req.Version)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "backup not found"})
		return
	}
	c.JSON(http.StatusOK, backup)
}

func (r *Router) listBackups(c *gin.Context) {
	backups, err := db.GetDeviceBackups(r.db, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if backups == nil {
		backups = []models.DeviceBackup{}
	}
	c.JSON(http.StatusOK, gin.H{"backups": backups})
}

// ── Auth middleware ──────────────────────────────────────────────

// ── Auth middleware (replaces old token-based version) ─────────────

func (r *Router) authMiddleware(c *gin.Context) {
	h := c.GetHeader("Authorization")
	if h == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization"})
		return
	}
	token := strings.TrimPrefix(h, "Bearer ")
	if token == h || token == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token format"})
		return
	}
	user, err := db.GetUserByToken(r.db, token)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	c.Set("user_id", user.ID)
	c.Next()
}

func (r *Router) deviceOwnerMiddleware(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no user context"})
		return
	}
	deviceID := c.Param("id")
	dev, err := db.GetDeviceByID(r.db, deviceID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "device not found"})
		return
	}
	if dev.UserID == nil || *dev.UserID != userID.(int64) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "not bound to this device"})
		return
	}
	c.Next()
}

// ── Device RPC ──────────────────────────────────────────────────

type DeviceRPCRequest struct {
	Method  string `json:"method" `
	Params  string `json:"params,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

func (r *Router) deviceRPC(c *gin.Context) {
	var req DeviceRPCRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rpcReq := &ws.RPCRequest{
		JSONRPC: "2.0",
		Method:  req.Method,
		ID:      1,
	}

	if req.Params != "" {
		rpcReq.Params = json.RawMessage(req.Params)
	} else {
		rpcReq.Params = json.RawMessage("{}")
	}

	deviceID := c.Param("id")
	resp, err := r.hub.SendRPC(deviceID, rpcReq)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	if resp.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error.Message})
		return
	}

	c.JSON(http.StatusOK, gin.H{"result": resp.Result})
}
