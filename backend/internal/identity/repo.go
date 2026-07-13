package identity

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *Repo) CreateUser(ctx context.Context, phone string, email *string, passwordHash string, role Role) (User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (phone, email, password_hash, role)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, phone, email, password_hash, role, created_at, updated_at`,
		phone, email, passwordHash, role,
	).Scan(&u.ID, &u.Phone, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrAlreadyExists
		}
		return User{}, err
	}
	return u, nil
}

func (r *Repo) GetUserByPhone(ctx context.Context, phone string) (User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, phone, email, password_hash, role, created_at, updated_at
		 FROM users WHERE phone = $1`,
		phone,
	).Scan(&u.ID, &u.Phone, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return u, nil
}

func (r *Repo) GetUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, phone, email, password_hash, role, created_at, updated_at
		 FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Phone, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return u, nil
}

func (r *Repo) CreateDriver(ctx context.Context, userID uuid.UUID, fullName, prdpNumber, idNumber string) (Driver, error) {
	var d Driver
	err := r.pool.QueryRow(ctx,
		`INSERT INTO drivers (user_id, full_name, prdp_number, id_number)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, user_id, full_name, prdp_number, prdp_verified, id_number, kyc_status, created_at, updated_at`,
		userID, fullName, prdpNumber, idNumber,
	).Scan(&d.ID, &d.UserID, &d.FullName, &d.PRDPNumber, &d.PRDPVerified, &d.IDNumber, &d.KYCStatus, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Driver{}, ErrAlreadyExists
		}
		return Driver{}, err
	}
	return d, nil
}

func (r *Repo) CreateVehicle(ctx context.Context, ownerUserID uuid.UUID, registration string, capacity int, associationName *string) (Vehicle, error) {
	var v Vehicle
	err := r.pool.QueryRow(ctx,
		`INSERT INTO vehicles (owner_user_id, registration, capacity, association_name)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, owner_user_id, registration, capacity, association_name, compliance_status, created_at, updated_at`,
		ownerUserID, registration, capacity, associationName,
	).Scan(&v.ID, &v.OwnerUserID, &v.Registration, &v.Capacity, &v.AssociationName, &v.ComplianceStatus, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Vehicle{}, ErrAlreadyExists
		}
		return Vehicle{}, err
	}
	return v, nil
}

func (r *Repo) CreateVehicleAssignment(ctx context.Context, vehicleID, driverID uuid.UUID) (VehicleAssignment, error) {
	var a VehicleAssignment
	err := r.pool.QueryRow(ctx,
		`INSERT INTO vehicle_assignments (vehicle_id, driver_id, active)
		 VALUES ($1, $2, true)
		 RETURNING id, vehicle_id, driver_id, active, created_at, updated_at`,
		vehicleID, driverID,
	).Scan(&a.ID, &a.VehicleID, &a.DriverID, &a.Active, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return VehicleAssignment{}, ErrAlreadyExists
		}
		return VehicleAssignment{}, err
	}
	return a, nil
}
