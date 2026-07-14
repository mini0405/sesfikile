package catalogue

import (
	"testing"

	"sesfikile/backend/internal/config"
)

func testFareModel() config.CatalogueFareModel {
	return config.CatalogueFareModel{
		BaseCents:    500,
		PerKmCents:   150,
		MinFareCents: 600,
		MaxFareCents: 6000,
	}
}

func TestEstimateFareCents_BaseCase(t *testing.T) {
	model := testFareModel()
	// 10km: 500 + 150*10 = 2000
	got := EstimateFareCents(10000, model)
	if got != 2000 {
		t.Errorf("expected 2000 cents for 10km, got %d", got)
	}
}

func TestEstimateFareCents_RoundsToNearestCent(t *testing.T) {
	model := testFareModel()
	// 1.234km: 500 + 150*1.234 = 500 + 185.1 = 685.1 -> rounds to 685
	got := EstimateFareCents(1234, model)
	if got != 685 {
		t.Errorf("expected 685 cents for 1.234km, got %d", got)
	}
}

func TestEstimateFareCents_ClampsToMinimum(t *testing.T) {
	model := testFareModel()
	// A tiny distance would compute below MinFareCents (500 + a few cents).
	got := EstimateFareCents(10, model)
	if got != model.MinFareCents {
		t.Errorf("expected fare clamped to MinFareCents (%d), got %d", model.MinFareCents, got)
	}
}

func TestEstimateFareCents_ClampsToMaximum(t *testing.T) {
	model := testFareModel()
	// A huge distance (147.7km, the real dataset's longest route) would
	// compute 500 + 150*147.7 = 22655, far above MaxFareCents.
	got := EstimateFareCents(147689, model)
	if got != model.MaxFareCents {
		t.Errorf("expected fare clamped to MaxFareCents (%d), got %d", model.MaxFareCents, got)
	}
}

func TestEstimateFareCents_NeverNegative(t *testing.T) {
	model := testFareModel()
	got := EstimateFareCents(0, model)
	if got < 0 {
		t.Errorf("expected a non-negative fare, got %d", got)
	}
	if got != model.MinFareCents {
		t.Errorf("expected a zero-distance route to clamp to MinFareCents (%d), got %d", model.MinFareCents, got)
	}
}
