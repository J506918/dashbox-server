package db

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"dashbox/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func Connect(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("gorm open: %w", err)
	}
	return db, nil
}

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.Device{},
		&models.DeviceParam{},
		&models.ParamHistory{},
		&models.UploadURL{},
		&models.User{},
		&models.DriveRoute{},
		&models.DeviceBackup{},
	)
}

// ── Devices ──────────────────────────────────────────────────────

func CreateDevice(db *gorm.DB, serial string) (*models.Device, error) {
	deviceID := generateDeviceID()
	dev := &models.Device{
		DeviceID:  deviceID,
		Serial:    serial,
		PublicKey: "",
		Token:     generateID("tok"),
	}
	if err := db.Create(dev).Error; err != nil {
		return nil, fmt.Errorf("create device: %w", err)
	}
	return dev, nil
}

func GetDeviceByID(db *gorm.DB, deviceID string) (*models.Device, error) {
	var dev models.Device
	if err := db.Where("device_id = ?", deviceID).First(&dev).Error; err != nil {
		return nil, err
	}
	return &dev, nil
}

func GetDeviceByToken(db *gorm.DB, token string) (*models.Device, error) {
	var dev models.Device
	if err := db.Where("token = ?", token).First(&dev).Error; err != nil {
		return nil, err
	}
	return &dev, nil
}

func GetDeviceByDeviceID(db *gorm.DB, deviceID string) (*models.Device, error) {
	var dev models.Device
	if err := db.Where("device_id = ?", deviceID).First(&dev).Error; err != nil {
		return nil, err
	}
	return &dev, nil
}

func GetDeviceBySerial(db *gorm.DB, serial string) (*models.Device, error) {
	var dev models.Device
	if err := db.Where("serial = ?", serial).First(&dev).Error; err != nil {
		return nil, err
	}
	return &dev, nil
}

func ListDevicesByUser(db *gorm.DB, userID int64) ([]models.Device, error) {
	var devs []models.Device
	if err := db.Where("user_id = ?", userID).Order("last_seen DESC").Find(&devs).Error; err != nil {
		return nil, err
	}
	return devs, nil
}

func UpdateDeviceOnline(db *gorm.DB, deviceID string, online bool) error {
	return db.Model(&models.Device{}).Where("device_id = ?", deviceID).
		Updates(map[string]interface{}{"online": online, "last_seen": gorm.Expr("NOW()")}).Error
}

func MarkAllOffline(db *gorm.DB) error {
	return db.Model(&models.Device{}).Where("online = ?", true).
		Update("online", false).Error
}

func UpdateDeviceInfo(db *gorm.DB, deviceID string, brand, model, version, branch, networkType string) error {
	return db.Model(&models.Device{}).Where("device_id = ?", deviceID).
		Updates(map[string]interface{}{
			"vehicle_brand":   brand,
			"vehicle_model":   model,
			"software_version": version,
			"branch":          branch,
			"network_type":    networkType,
		}).Error
}

func UpdateDeviceNetwork(db *gorm.DB, deviceID, networkType string) error {
	return db.Model(&models.Device{}).Where("device_id = ?", deviceID).
		Update("network_type", networkType).Error
}

func UpdateDeviceVehicle(db *gorm.DB, deviceID, brand, model, version, branch string) error {
	return db.Model(&models.Device{}).Where("device_id = ?", deviceID).
		Updates(map[string]interface{}{
			"vehicle_brand":   brand,
			"vehicle_model":   model,
			"software_version": version,
			"branch":          branch,
		}).Error
}

// ── Params ───────────────────────────────────────────────────────

func GetDeviceParams(db *gorm.DB, deviceID string) (map[string]string, error) {
	var rows []models.DeviceParam
	if err := db.Where("device_id = ?", deviceID).Find(&rows).Error; err != nil {
		return nil, err
	}
	params := make(map[string]string, len(rows))
	for _, r := range rows {
		params[r.Key] = r.Value
	}
	return params, nil
}

func SaveDeviceParams(db *gorm.DB, deviceID string, params map[string]string) error {
	for key, value := range params {
		var existing models.DeviceParam
		err := db.Where("device_id = ? AND key = ?", deviceID, key).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			if err := db.Create(&models.DeviceParam{
				DeviceID: deviceID,
				Key:      key,
				Value:    value,
			}).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if existing.Value != value {
				if err := db.Model(&existing).Update("value", value).Error; err != nil {
					return err
				}
			}
		}
		db.Create(&models.ParamHistory{
			DeviceID: deviceID,
			Key:      key,
			Value:    value,
		})
	}
	return nil
}

// ── Users ────────────────────────────────────────────────────────

func GetUserByPhone(db *gorm.DB, phone string) (*models.User, error) {
	var user models.User
	if err := db.Where("phone = ?", phone).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func GetUserByToken(db *gorm.DB, token string) (*models.User, error) {
	var user models.User
	if err := db.Where("token = ?", token).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func CreateUser(db *gorm.DB, phone, nickname string) (*models.User, string, error) {
	token := generateID("usr")
	user := &models.User{
		Phone:    phone,
		Nickname: nickname,
		Token:    token,
	}
	if err := db.Create(user).Error; err != nil {
		return nil, "", fmt.Errorf("create user: %w", err)
	}
	return user, token, nil
}

func GetOrCreateUserByQQ(db *gorm.DB, openID, nickname string) (*models.User, string, error) {
	var user models.User
	err := db.Where("qq_open_id = ?", openID).First(&user).Error
	if err == nil {
		if nickname != "" && user.Nickname != nickname {
			db.Model(&user).Update("nickname", nickname)
		}
		return &user, user.Token, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, "", fmt.Errorf("query user by qq: %w", err)
	}
	token := generateID("usr")
	user = models.User{
		QQOpenID: openID,
		Nickname: nickname,
		Token:    token,
	}
	if err := db.Create(&user).Error; err != nil {
		return nil, "", fmt.Errorf("create qq user: %w", err)
	}
	return &user, token, nil
}

// ── Helpers ──────────────────────────────────────────────────────

func generateDeviceID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateID(prefix string) string {
	b := make([]byte, 16)
	rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func generatePairingCode() string {
	b := make([]byte, 3)
	rand.Read(b)
	n := (int(b[0])<<16 | int(b[1])<<8 | int(b[2])) % 1000000
	return fmt.Sprintf("%06d", n)
}

// ── Pairing ──────────────────────────────────────────────────────

func GeneratePairingCode(db *gorm.DB, deviceID string) (string, error) {
	// Return existing valid code if still active
	var dev models.Device
	err := db.Where("device_id = ? AND pairing_code != '' AND pairing_exp > ?", deviceID, time.Now()).
		First(&dev).Error
	if err == nil {
		return dev.PairingCode, nil
	}

	code := generatePairingCode()
	exp := time.Now().Add(15 * time.Minute)
	if err := db.Model(&models.Device{}).Where("device_id = ?", deviceID).
		Updates(map[string]interface{}{
			"pairing_code": code,
			"pairing_exp":  exp,
		}).Error; err != nil {
		return "", err
	}
	return code, nil
}

func VerifyPairingCode(db *gorm.DB, code string) (*models.Device, error) {
	var dev models.Device
	if err := db.Where("pairing_code = ? AND pairing_exp > ?", code, time.Now()).
		First(&dev).Error; err != nil {
		return nil, err
	}
	return &dev, nil
}

func BindDevice(db *gorm.DB, deviceID string, userID int64) error {
	return db.Model(&models.Device{}).Where("device_id = ?", deviceID).
		Updates(map[string]interface{}{
			"user_id":      userID,
			"pairing_code": "",
		}).Error
}

func MarkDeviceBound(db *gorm.DB, deviceID string) error {
	return db.Model(&models.Device{}).Where("device_id = ?", deviceID).
		Update("pairing_code", "").Error
}

// ── Routes ────────────────────────────────────────────────────────

func GetDeviceRoutes(db *gorm.DB, deviceID string) ([]models.DriveRoute, error) {
	var routes []models.DriveRoute
	if err := db.Where("device_id = ?", deviceID).Order("start_time DESC").Limit(50).Find(&routes).Error; err != nil {
		return nil, err
	}
	return routes, nil
}

func CreateRoute(db *gorm.DB, deviceID string, startTime, endTime time.Time, distance float64, duration int, routeData string) error {
	return db.Create(&models.DriveRoute{
		DeviceID:  deviceID,
		StartTime: startTime,
		EndTime:   endTime,
		Distance:  distance,
		Duration:  duration,
		RouteData: routeData,
	}).Error
}

// ── Backups ───────────────────────────────────────────────────────

func GetDeviceBackups(db *gorm.DB, deviceID string) ([]models.DeviceBackup, error) {
	var backups []models.DeviceBackup
	if err := db.Where("device_id = ?", deviceID).Order("created_at DESC").Limit(20).Find(&backups).Error; err != nil {
		return nil, err
	}
	return backups, nil
}

func CreateBackup(db *gorm.DB, deviceID string, data string, size int) (*models.DeviceBackup, error) {
	var last models.DeviceBackup
	db.Where("device_id = ?", deviceID).Order("version DESC").First(&last)
	version := last.Version + 1
	backup := &models.DeviceBackup{
		DeviceID: deviceID,
		Version:  version,
		Size:     size,
		Data:     data,
	}
	if err := db.Create(backup).Error; err != nil {
		return nil, err
	}
	return backup, nil
}

func GetBackup(db *gorm.DB, deviceID string, version int) (*models.DeviceBackup, error) {
	var backup models.DeviceBackup
	if err := db.Where("device_id = ? AND version = ?", deviceID, version).First(&backup).Error; err != nil {
		return nil, err
	}
	return &backup, nil
}
