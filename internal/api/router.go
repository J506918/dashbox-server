package api

import (
	"dashbox/internal/ws"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Router struct {
	db  *gorm.DB
	hub *ws.Hub
	*gin.Engine
}

func NewRouter(database *gorm.DB, hub *ws.Hub) *Router {
	r := &Router{
		db:    database,
		hub:   hub,
		Engine: gin.Default(),
	}
	r.setupRoutes()
	return r
}

func (r *Router) setupRoutes() {
	api := r.Group("/api/v1")
	{
		api.GET("/health", r.health)

		// Public — device registration
		api.POST("/devices/register", r.registerDevice)

		// Public — pairing code verification (no auth needed)
		api.POST("/pair/verify", r.verifyPairingCode)

		// Public — device requests pairing code (auth via serial+dongle_id)
		api.POST("/devices/pair", r.deviceGeneratePairingCode)

		// ── Authenticated routes (require user JWT) ──
		auth := api.Group("")
		auth.Use(r.authMiddleware)
		{
			// Device list (shows user's bound devices)
			auth.GET("/devices", r.listDevices)

			// Bind device (requires user auth + three-element verification)
			auth.POST("/pair/bind", r.bindDevice)

			// Generate pairing code (requires auth, but NOT device ownership — device may be unbound)
			auth.POST("/devices/:id/pair", r.generatePairingCode)

			// ── Device-specific routes (require device ownership) ──
			device := auth.Group("/devices/:id")
			device.Use(r.deviceOwnerMiddleware)
			{
				device.GET("", r.getDevice)
				device.GET("/status", r.deviceStatus)
				device.GET("/params", r.getDeviceParams)
				device.POST("/params", r.saveDeviceParams)
				device.POST("/rpc", r.deviceRPC)
				device.GET("/routes", r.getDriveRoutes)
				device.POST("/routes", r.createDriveRoute)
				device.GET("/backups", r.listBackups)
				device.POST("/backups", r.createBackup)
			device.GET("/backups/:version", r.getBackup)
		}
		}
	}

	// QQ OAuth (browser redirect, no auth)
	r.GET("/auth/qq/callback", r.qqCallback)
	r.GET("/api/auth/qq/token", r.qqTokenPoll)

	// WebSocket — device and app connections
	r.GET("/ws", r.wsHandler)
	r.GET("/ws/app", r.wsAppHandler)
}
