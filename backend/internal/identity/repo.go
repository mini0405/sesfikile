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

func (r *Repo) GetDriverByUserID(ctx context.Context, userID uuid.UUID) (Driver, error) {
	var d Driver
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, full_name, prdp_number, prdp_verified, id_number, kyc_status, created_at, updated_at
		 FROM drivers WHERE user_id = $1`,
		userID,
	).Scan(&d.ID, &d.UserID, &d.FullName, &d.PRDPNumber, &d.PRDPVerified, &d.IDNumber, &d.KYCStatus, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Driver{}, ErrNotFound
		}
		return Driver{}, err
	}
	return d, nil
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

func (r *Repo) GetVehicleByRegistration(ctx context.Context, registration string) (Vehicle, error) {
	var v Vehicle
	err := r.pool.QueryRow(ctx,
		`SELECT id, owner_user_id, registration, capacity, association_name, compliance_status, created_at, updated_at
		 FROM vehicles WHERE registration = $1`,
		registration,
	).Scan(&v.ID, &v.OwnerUserID, &v.Registration, &v.Capacity, &v.AssociationName, &v.ComplianceStatus, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Vehicle{}, ErrNotFound
		}
		return Vehicle{}, err
	}
	return v, nil
}

func (r *Repo) GetVehicleByID(ctx context.Context, id uuid.UUID) (Vehicle, error) {
	var v Vehicle
	err := r.pool.QueryRow(ctx,
		`SELECT id, owner_user_id, registration, capacity, association_name, compliance_status, created_at, updated_at
		 FROM vehicles WHERE id = $1`,
		id,
	).Scan(&v.ID, &v.OwnerUserID, &v.Registration, &v.Capacity, &v.AssociationName, &v.ComplianceStatus, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Vehicle{}, ErrNotFound
		}
		return Vehicle{}, err
	}
	return v, nil
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

// ListVehiclesByOwnerUserID returns every vehicle owned by ownerUserID,
// ordered by registration. Used by the Stage 8 owner-analytics read paths —
// a plain scoped list query, no aggregation.
func (r *Repo) ListVehiclesByOwnerUserID(ctx context.Context, ownerUserID uuid.UUID) ([]Vehicle, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, owner_user_id, registration, capacity, association_name, compliance_status, created_at, updated_at
		 FROM vehicles WHERE owner_user_id = $1 ORDER BY registration`,
		ownerUserID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vehicles []Vehicle
	for rows.Next() {
		var v Vehicle
		if err := rows.Scan(&v.ID, &v.OwnerUserID, &v.Registration, &v.Capacity, &v.AssociationName, &v.ComplianceStatus, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		vehicles = append(vehicles, v)
	}
	return vehicles, rows.Err()
}

// GetActiveAssignmentByVehicleID returns a vehicle's current active driver
// assignment, the mirror of GetActiveVehicleAssignmentByDriverID keyed the
// other direction. There is at most one, enforced by the partial unique
// index on vehicle_assignments (vehicle_id WHERE active).
func (r *Repo) GetActiveAssignmentByVehicleID(ctx context.Context, vehicleID uuid.UUID) (VehicleAssignment, error) {
	var a VehicleAssignment
	err := r.pool.QueryRow(ctx,
		`SELECT id, vehicle_id, driver_id, active, created_at, updated_at
		 FROM vehicle_assignments WHERE vehicle_id = $1 AND active = true`,
		vehicleID,
	).Scan(&a.ID, &a.VehicleID, &a.DriverID, &a.Active, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return VehicleAssignment{}, ErrNotFound
		}
		return VehicleAssignment{}, err
	}
	return a, nil
}

// GetDriverByID looks up a driver profile by its own id (as opposed to
// GetDriverByUserID, keyed by the underlying user account).
func (r *Repo) GetDriverByID(ctx context.Context, id uuid.UUID) (Driver, error) {
	var d Driver
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, full_name, prdp_number, prdp_verified, id_number, kyc_status, created_at, updated_at
		 FROM drivers WHERE id = $1`,
		id,
	).Scan(&d.ID, &d.UserID, &d.FullName, &d.PRDPNumber, &d.PRDPVerified, &d.IDNumber, &d.KYCStatus, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Driver{}, ErrNotFound
		}
		return Driver{}, err
	}
	return d, nil
}

// ListDriversByOwnerUserID returns every driver currently actively assigned
// to one of ownerUserID's vehicles, ordered by full name.
func (r *Repo) ListDriversByOwnerUserID(ctx context.Context, ownerUserID uuid.UUID) ([]Driver, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT d.id, d.user_id, d.full_name, d.prdp_number, d.prdp_verified, d.id_number, d.kyc_status, d.created_at, d.updated_at
		 FROM drivers d
		 JOIN vehicle_assignments va ON va.driver_id = d.id AND va.active
		 JOIN vehicles v ON v.id = va.vehicle_id
		 WHERE v.owner_user_id = $1
		 ORDER BY d.full_name`,
		ownerUserID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var drivers []Driver
	for rows.Next() {
		var d Driver
		if err := rows.Scan(&d.ID, &d.UserID, &d.FullName, &d.PRDPNumber, &d.PRDPVerified, &d.IDNumber, &d.KYCStatus, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		drivers = append(drivers, d)
	}
	return drivers, rows.Err()
}

// GetActiveVehicleAssignmentByDriverID returns the driver's current active
// vehicle assignment. There is at most one, enforced by the partial unique
// index on vehicle_assignments (driver_id WHERE active).
func (r *Repo) GetActiveVehicleAssignmentByDriverID(ctx context.Context, driverID uuid.UUID) (VehicleAssignment, error) {
	var a VehicleAssignment
	err := r.pool.QueryRow(ctx,
		`SELECT id, vehicle_id, driver_id, active, created_at, updated_at
		 FROM vehicle_assignments WHERE driver_id = $1 AND active = true`,
		driverID,
	).Scan(&a.ID, &a.VehicleID, &a.DriverID, &a.Active, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return VehicleAssignment{}, ErrNotFound
		}
		return VehicleAssignment{}, err
	}
	return a, nil
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
