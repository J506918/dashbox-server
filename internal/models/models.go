package models

import "time"

// Device represents a registered comma device.
type Device struct {
	ID             int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceID       string    `json:"device_id" gorm:"uniqueIndex;size:64;not null"`
	Serial         string    `json:"serial" gorm:"size:128;not null"`
	IMEI           string    `json:"imei" gorm:"size:64"`
	PublicKey      string    `json:"public_key" gorm:"type:text;not null"`
	Token          string    `json:"token" gorm:"uniqueIndex;size:128;not null"`
	PairingCode    string    `json:"pairing_code" gorm:"size:6"`
	PairingExp     time.Time `json:"pairing_exp"`
	UserID         *int64    `json:"user_id" gorm:"index"`
	LastSeen       time.Time `json:"last_seen"`
	Online         bool      `json:"online" gorm:"default:false"`
	NetworkType    string    `json:"network_type" gorm:"size:16"`
	VehicleBrand   string    `json:"vehicle_brand" gorm:"size:64"`
	VehicleModel   string    `json:"vehicle_model" gorm:"size:128"`
	SoftwareVersion string   `json:"software_version" gorm:"size:64"`
	Branch         string    `json:"branch" gorm:"size:128"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type DeviceParam struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceID  string    `json:"device_id" gorm:"index;size:64;not null"`
	Key       string    `json:"key" gorm:"size:256;not null"`
	Value     string    `json:"value" gorm:"type:text"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ParamHistory struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceID  string    `json:"device_id" gorm:"index;size:64;not null"`
	Key       string    `json:"key" gorm:"size:256;not null"`
	Value     string    `json:"value" gorm:"type:text"`
	ChangedAt time.Time `json:"changed_at"`
}

type UploadURL struct {
	DeviceID  string    `json:"device_id" gorm:"index;size:64;not null"`
	Path      string    `json:"path" gorm:"size:512;not null"`
	URL       string    `json:"url" gorm:"type:text;not null"`
	ExpiresAt time.Time `json:"expires_at"`
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement"`
}

type User struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	Phone     string    `json:"phone" gorm:"index;size:20"`
	QQOpenID  string    `json:"qq_open_id" gorm:"uniqueIndex;size:64"`
	Nickname  string    `json:"nickname" gorm:"size:64"`
	Token     string    `json:"token" gorm:"uniqueIndex;size:128"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DriveRoute represents a recorded driving route.
type DriveRoute struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceID  string    `json:"device_id" gorm:"index;size:64;not null"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Distance  float64   `json:"distance" gorm:"default:0"`
	Duration  int       `json:"duration" gorm:"default:0"`
	RouteData string    `json:"route_data" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at"`
}

// DeviceBackup represents a device settings backup.
type DeviceBackup struct {
	ID        int64     `json:"id" gorm:"primaryKey;autoIncrement"`
	DeviceID  string    `json:"device_id" gorm:"index;size:64;not null"`
	Version   int       `json:"version" gorm:"default:0"`
	Size      int       `json:"size" gorm:"default:0"`
	Data      string    `json:"data" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at"`
}
