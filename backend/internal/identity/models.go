package identity

import (
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleCommuter Role = "commuter"
	RoleDriver   Role = "driver"
	RoleOwner    Role = "owner"
)

func (r Role) Valid() bool {
	switch r {
	case RoleCommuter, RoleDriver, RoleOwner:
		return true
	default:
		return false
	}
}

type KYCStatus string

const (
	KYCPending  KYCStatus = "pending"
	KYCVerified KYCStatus = "verified"
	KYCRejected KYCStatus = "rejected"
)

type ComplianceStatus string

const (
	ComplianceStatusPending  ComplianceStatus = "pending"
	ComplianceStatusVerified ComplianceStatus = "verified"
)

type User struct {
	ID           uuid.UUID
	Phone        string
	Email        *string
	PasswordHash string
	Role         Role
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// PRDPVerified and KYCStatus are stored fields only for the MVP — no real
// verification workflow is wired up yet (see CLAUDE.md "SCOPE HONESTY").
type Driver struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	FullName      string
	PRDPNumber    string
	PRDPVerified  bool
	IDNumber      string
	KYCStatus     KYCStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Vehicle struct {
	ID                uuid.UUID
	OwnerUserID       uuid.UUID
	Registration      string
	Capacity          int
	AssociationName   *string
	ComplianceStatus  ComplianceStatus
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type VehicleAssignment struct {
	ID        uuid.UUID
	VehicleID uuid.UUID
	DriverID  uuid.UUID
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}
