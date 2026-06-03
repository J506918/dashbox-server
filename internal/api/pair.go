package api

import (
	"log"
	"net/http"

	"dashbox/internal/db"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (r *Router) generatePairingCode(c *gin.Context) {
	deviceID := c.Param("id")
	log.Printf("[pair] generating code for device: %s", deviceID)

	code, err := db.GeneratePairingCode(r.db, deviceID)
	if err != nil {
		log.Printf("[pair] generate failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate code"})
		return
	}

	log.Printf("[pair] code %s generated for %s", code, deviceID)
	c.JSON(http.StatusOK, gin.H{
		"pairing_code": code,
		"expires_in":   900,
	})
}

func (r *Router) verifyPairingCode(c *gin.Context) {
	var req struct {
		PairingCode string `json:"pairing_code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing pairing_code"})
		return
	}

	dev, err := db.VerifyPairingCode(r.db, req.PairingCode)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "invalid or expired pairing code"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"device_id": dev.DeviceID,
		"serial":    dev.Serial,
	})
}

func (r *Router) bindDevice(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "login required"})
		return
	}

	var req struct {
		Serial      string `json:"serial" binding:"required"`
		DongleID    string `json:"dongle_id" binding:"required"`
		PairingCode string `json:"pairing_code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing serial, dongle_id, or pairing_code"})
		return
	}

	dev, err := db.VerifyPairingCode(r.db, req.PairingCode)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "invalid or expired pairing code"})
		return
	}

	if dev.Serial != req.Serial {
		c.JSON(http.StatusForbidden, gin.H{"error": "serial mismatch"})
		return
	}

	if dev.DeviceID != req.DongleID {
		c.JSON(http.StatusForbidden, gin.H{"error": "dongle_id mismatch"})
		return
	}

	// Bind device to authenticated user (sets user_id + clears pairing code)
	if err := db.BindDevice(r.db, dev.DeviceID, userID.(int64)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bind failed"})
		return
	}

	log.Printf("[pair] device %s bound to user %d", dev.DeviceID, userID)

	c.JSON(http.StatusOK, gin.H{
		"status":    "bound",
		"device_id": dev.DeviceID,
		"serial":    dev.Serial,
	})
}

func (r *Router) deviceGeneratePairingCode(c *gin.Context) {
	var req struct {
		Serial   string `json:"serial" binding:"required"`
		DongleID string `json:"dongle_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing serial or dongle_id"})
		return
	}

	dev, err := db.GetDeviceBySerial(r.db, req.Serial)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "device not registered"})
		return
	}
	if dev.DeviceID != req.DongleID {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "dongle_id mismatch"})
		return
	}

	code, err := db.GeneratePairingCode(r.db, dev.DeviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate code"})
		return
	}

	log.Printf("[pair] device %s requested code: %s", dev.DeviceID, code)
	c.JSON(http.StatusOK, gin.H{
		"pairing_code": code,
		"expires_in":   900,
	})
}

