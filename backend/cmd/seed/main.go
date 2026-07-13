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
	"sesfikile/backend/internal/routing"
	"sesfikile/backend/internal/wallet"
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
	walletRepo := wallet.NewRepo(pool)
	routingRepo := routing.NewRepo(pool)

	if err := walletRepo.EnsureSystemAccounts(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to seed system accounts: %v\n", err)
		os.Exit(1)
	}

	owner := seedUser{"owner", "+27820000001", "owner@sesfikile.dev", "Owner123!", identity.RoleOwner}
	driver1 := seedUser{"driver 1", "+27820000002", "driver1@sesfikile.dev", "Driver123!", identity.RoleDriver}
	driver2 := seedUser{"driver 2", "+27820000003", "driver2@sesfikile.dev", "Driver123!", identity.RoleDriver}
	commuter1 := seedUser{"commuter 1", "+27820000004", "commuter1@sesfikile.dev", "Commuter123!", identity.RoleCommuter}
	commuter2 := seedUser{"commuter 2", "+27820000005", "commuter2@sesfikile.dev", "Commuter123!", identity.RoleCommuter}

	ownerUser := seedOrGetUser(ctx, repo, owner)
	driver1User := seedOrGetUser(ctx, repo, driver1)
	driver2User := seedOrGetUser(ctx, repo, driver2)
	commuter1User := seedOrGetUser(ctx, repo, commuter1)
	commuter2User := seedOrGetUser(ctx, repo, commuter2)

	driver1Record := seedOrGetDriver(ctx, repo, driver1User.ID, "Thabo Nkosi", "PRDP0001", "8001015800083")
	driver2Record := seedOrGetDriver(ctx, repo, driver2User.ID, "Sipho Dlamini", "PRDP0002", "8505124800089")

	assoc := "Cape Town Minibus Taxi Association"
	vehicle1 := seedOrGetVehicle(ctx, repo, ownerUser.ID, "CA123456", 16, &assoc)
	vehicle2 := seedOrGetVehicle(ctx, repo, ownerUser.ID, "CA654321", 16, &assoc)

	if _, err := repo.CreateVehicleAssignment(ctx, vehicle1.ID, driver1Record.ID); err != nil && !errors.Is(err, identity.ErrAlreadyExists) {
		fmt.Fprintf(os.Stderr, "failed to assign driver 1 to vehicle 1: %v\n", err)
		os.Exit(1)
	}
	if _, err := repo.CreateVehicleAssignment(ctx, vehicle2.ID, driver2Record.ID); err != nil && !errors.Is(err, identity.ErrAlreadyExists) {
		fmt.Fprintf(os.Stderr, "failed to assign driver 2 to vehicle 2: %v\n", err)
		os.Exit(1)
	}

	// Starting wallet balance is seeded via a real top-up transaction (not a
	// raw balance write), so it's a normal, invariant-respecting ledger entry.
	// Only top up once per commuter — re-running seed must stay a no-op, and
	// a top-up has no idempotency key to dedupe on, so we gate on balance.
	const startingBalanceCents = 10000 // R100.00
	for _, u := range []identity.User{commuter1User, commuter2User} {
		acc, err := walletRepo.GetOrCreateAccount(ctx, pool, &u.ID, wallet.AccountCommuterWallet)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load wallet account for %s: %v\n", u.Phone, err)
			os.Exit(1)
		}
		balance, err := walletRepo.AccountBalance(ctx, pool, acc.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read balance for %s: %v\n", u.Phone, err)
			os.Exit(1)
		}
		if balance > 0 {
			continue
		}
		if _, _, err := walletRepo.Topup(ctx, u.ID, startingBalanceCents); err != nil {
			fmt.Fprintf(os.Stderr, "failed to seed starting balance for %s: %v\n", u.Phone, err)
			os.Exit(1)
		}
	}

	if err := routing.SeedCorridors(ctx, routingRepo); err != nil {
		fmt.Fprintf(os.Stderr, "failed to seed routing corridors: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Seed complete.")
	fmt.Println()
	fmt.Println("SEEDED DATA")
	fmt.Println("===========")
	fmt.Println()

	fmt.Println("Logins (POST /auth/login with phone + password):")
	users := []struct {
		seedUser
		user identity.User
	}{
		{owner, ownerUser},
		{driver1, driver1User},
		{driver2, driver2User},
		{commuter1, commuter1User},
		{commuter2, commuter2User},
	}
	for _, u := range users {
		fmt.Printf("  %-12s phone=%-15s password=%-14s role=%-9s user_id=%s\n",
			u.label, u.phone, u.password, u.role, u.user.ID)
	}
	fmt.Println()

	fmt.Println("Vehicles:")
	fmt.Printf("  %-10s id=%s   owner=%s (%s)\n", vehicle1.Registration, vehicle1.ID, owner.label, ownerUser.ID)
	fmt.Printf("  %-10s id=%s   owner=%s (%s)\n", vehicle2.Registration, vehicle2.ID, owner.label, ownerUser.ID)
	fmt.Println()

	fmt.Println("Drivers:")
	fmt.Printf("  %-14s id=%s   user_id=%s   vehicle=%s (%s)\n",
		driver1Record.FullName, driver1Record.ID, driver1User.ID, vehicle1.Registration, vehicle1.ID)
	fmt.Printf("  %-14s id=%s   user_id=%s   vehicle=%s (%s)\n",
		driver2Record.FullName, driver2Record.ID, driver2User.ID, vehicle2.Registration, vehicle2.ID)
	fmt.Println()

	printRoutingSummary(ctx, routingRepo)
}

// printRoutingSummary prints the seeded stops, routes (with ordered
// legs/fares), and a note on which stops are interchanges — everything a
// developer needs to exercise GET /routes/search by hand.
func printRoutingSummary(ctx context.Context, repo *routing.Repo) {
	stops, err := repo.ListStops(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to list stops: %v\n", err)
		os.Exit(1)
	}
	routes, err := repo.AllRoutesWithLegs(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to list routes: %v\n", err)
		os.Exit(1)
	}

	stopNames := map[uuid.UUID]string{}
	for _, s := range stops {
		stopNames[s.ID] = s.Name
	}

	fmt.Println("Stops:")
	for _, s := range stops {
		fmt.Printf("  %-30s id=%s\n", s.Name, s.ID)
	}
	fmt.Println()

	fmt.Println("Routes:")
	for _, rwl := range routes {
		fmt.Printf("  %s (%s) id=%s\n", rwl.Route.Name, rwl.Route.AssociationName, rwl.Route.ID)
		for _, l := range rwl.Legs {
			fmt.Printf("    seq=%d  %s -> %s  fare_cents=%d\n", l.Sequence, stopNames[l.FromStopID], stopNames[l.ToStopID], l.FareCents)
		}
	}
	fmt.Println()

	// Interchanges are computed from ForwardCorridors only, not from every
	// seeded route row — a corridor and its own return-trip route share
	// every stop by construction, which would otherwise make every stop
	// look like an "interchange". See routing.ForwardCorridors' doc comment.
	fmt.Println("Interchanges (stops shared by more than one corridor):")
	corridorsByStop := map[string]int{}
	for _, corridor := range routing.ForwardCorridors {
		seen := map[string]bool{}
		mark := func(stopName string) {
			if !seen[stopName] {
				seen[stopName] = true
				corridorsByStop[stopName]++
			}
		}
		if len(corridor.Legs) > 0 {
			mark(corridor.Legs[0].FromStop)
		}
		for _, l := range corridor.Legs {
			mark(l.ToStop)
		}
	}
	for _, s := range stops {
		if corridorsByStop[s.Name] > 1 {
			fmt.Printf("  %-30s id=%s\n", s.Name, s.ID)
		}
	}
}

func seedOrGetDriver(ctx context.Context, repo *identity.Repo, userID uuid.UUID, fullName, prdpNumber, idNumber string) identity.Driver {
	driver, err := repo.CreateDriver(ctx, userID, fullName, prdpNumber, idNumber)
	if err == nil {
		return driver
	}
	if !errors.Is(err, identity.ErrAlreadyExists) {
		fmt.Fprintf(os.Stderr, "failed to seed driver profile for %s: %v\n", fullName, err)
		os.Exit(1)
	}

	existing, err := repo.GetDriverByUserID(ctx, userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load existing driver profile for %s: %v\n", fullName, err)
		os.Exit(1)
	}
	return existing
}

func seedOrGetVehicle(ctx context.Context, repo *identity.Repo, ownerUserID uuid.UUID, registration string, capacity int, associationName *string) identity.Vehicle {
	vehicle, err := repo.CreateVehicle(ctx, ownerUserID, registration, capacity, associationName)
	if err == nil {
		return vehicle
	}
	if !errors.Is(err, identity.ErrAlreadyExists) {
		fmt.Fprintf(os.Stderr, "failed to seed vehicle %s: %v\n", registration, err)
		os.Exit(1)
	}

	existing, err := repo.GetVehicleByRegistration(ctx, registration)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load existing vehicle %s: %v\n", registration, err)
		os.Exit(1)
	}
	return existing
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
