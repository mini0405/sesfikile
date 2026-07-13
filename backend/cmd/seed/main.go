// Command seed populates the identity tables with known dev accounts:
// 1 owner, 2 vehicles, 2 drivers (each assigned to a vehicle), and 2
// commuters. Safe to re-run — existing rows are left untouched.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"sesfikile/backend/internal/config"
	"sesfikile/backend/internal/db"
	"sesfikile/backend/internal/identity"
)

type seedUser struct {
	label    string
	phone    string
	email    string
	password string
	role     identity.Role
}

func main() {
	cfg := config.Load()
	ctx := context.Background()

	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "failed to apply migrations: %v\n", err)
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	repo := identity.NewRepo(pool)

	owner := seedUser{"owner", "+27820000001", "owner@sesfikile.dev", "Owner123!", identity.RoleOwner}
	driver1 := seedUser{"driver 1", "+27820000002", "driver1@sesfikile.dev", "Driver123!", identity.RoleDriver}
	driver2 := seedUser{"driver 2", "+27820000003", "driver2@sesfikile.dev", "Driver123!", identity.RoleDriver}
	commuter1 := seedUser{"commuter 1", "+27820000004", "commuter1@sesfikile.dev", "Commuter123!", identity.RoleCommuter}
	commuter2 := seedUser{"commuter 2", "+27820000005", "commuter2@sesfikile.dev", "Commuter123!", identity.RoleCommuter}

	ownerUser := seedOrGetUser(ctx, repo, owner)
	driver1User := seedOrGetUser(ctx, repo, driver1)
	driver2User := seedOrGetUser(ctx, repo, driver2)
	seedOrGetUser(ctx, repo, commuter1)
	seedOrGetUser(ctx, repo, commuter2)

	driver1Record, err := repo.CreateDriver(ctx, driver1User.ID, "Thabo Nkosi", "PRDP0001", "8001015800083")
	if err != nil && !errors.Is(err, identity.ErrAlreadyExists) {
		fmt.Fprintf(os.Stderr, "failed to seed driver 1 profile: %v\n", err)
		os.Exit(1)
	}
	driver2Record, err := repo.CreateDriver(ctx, driver2User.ID, "Sipho Dlamini", "PRDP0002", "8505124800089")
	if err != nil && !errors.Is(err, identity.ErrAlreadyExists) {
		fmt.Fprintf(os.Stderr, "failed to seed driver 2 profile: %v\n", err)
		os.Exit(1)
	}

	assoc := "Cape Town Minibus Taxi Association"
	vehicle1, err := repo.CreateVehicle(ctx, ownerUser.ID, "CA123456", 16, &assoc)
	if err != nil && !errors.Is(err, identity.ErrAlreadyExists) {
		fmt.Fprintf(os.Stderr, "failed to seed vehicle 1: %v\n", err)
		os.Exit(1)
	}
	vehicle2, err := repo.CreateVehicle(ctx, ownerUser.ID, "CA654321", 16, &assoc)
	if err != nil && !errors.Is(err, identity.ErrAlreadyExists) {
		fmt.Fprintf(os.Stderr, "failed to seed vehicle 2: %v\n", err)
		os.Exit(1)
	}

	if driver1Record.ID != uuid.Nil && vehicle1.ID != uuid.Nil {
		if _, err := repo.CreateVehicleAssignment(ctx, vehicle1.ID, driver1Record.ID); err != nil && !errors.Is(err, identity.ErrAlreadyExists) {
			fmt.Fprintf(os.Stderr, "failed to assign driver 1 to vehicle 1: %v\n", err)
			os.Exit(1)
		}
	}
	if driver2Record.ID != uuid.Nil && vehicle2.ID != uuid.Nil {
		if _, err := repo.CreateVehicleAssignment(ctx, vehicle2.ID, driver2Record.ID); err != nil && !errors.Is(err, identity.ErrAlreadyExists) {
			fmt.Fprintf(os.Stderr, "failed to assign driver 2 to vehicle 2: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("Seed complete. Dev logins (POST /auth/login with phone + password):")
	fmt.Println()
	for _, u := range []seedUser{owner, driver1, driver2, commuter1, commuter2} {
		fmt.Printf("  %-12s phone=%-15s password=%-14s role=%s\n", u.label, u.phone, u.password, u.role)
	}
}

func seedOrGetUser(ctx context.Context, repo *identity.Repo, u seedUser) identity.User {
	hash, err := identity.HashPassword(u.password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to hash password for %s: %v\n", u.label, err)
		os.Exit(1)
	}

	email := u.email
	user, err := repo.CreateUser(ctx, u.phone, &email, hash, u.role)
	if err == nil {
		return user
	}
	if !errors.Is(err, identity.ErrAlreadyExists) {
		fmt.Fprintf(os.Stderr, "failed to seed %s: %v\n", u.label, err)
		os.Exit(1)
	}

	existing, err := repo.GetUserByPhone(ctx, u.phone)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load existing %s: %v\n", u.label, err)
		os.Exit(1)
	}
	return existing
}
