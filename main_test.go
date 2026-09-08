package main

import (
	"testing"
	"time"
)

func TestGetEnv(t *testing.T) {
	t.Run("returns fallback when key unset", func(t *testing.T) {
		got := getEnv("TEST_UNSET_KEY_XYZ", "default_val")
		if got != "default_val" {
			t.Errorf("expected default_val, got %s", got)
		}
	})

	t.Run("returns fallback when key is empty string", func(t *testing.T) {
		t.Setenv("TEST_EMPTY_KEY", "")
		got := getEnv("TEST_EMPTY_KEY", "default_val")
		if got != "default_val" {
			t.Errorf("expected default_val for empty env var, got %s", got)
		}
	})

	t.Run("returns value when set", func(t *testing.T) {
		t.Setenv("TEST_SET_KEY", "custom_val")
		got := getEnv("TEST_SET_KEY", "default_val")
		if got != "custom_val" {
			t.Errorf("expected custom_val, got %s", got)
		}
	})
}

func TestGetEnvInt(t *testing.T) {
	t.Run("returns fallback when unset", func(t *testing.T) {
		got := getEnvInt("TEST_UNSET_INT", 42)
		if got != 42 {
			t.Errorf("expected 42, got %d", got)
		}
	})

	t.Run("returns fallback when empty", func(t *testing.T) {
		t.Setenv("TEST_EMPTY_INT", "")
		got := getEnvInt("TEST_EMPTY_INT", 42)
		if got != 42 {
			t.Errorf("expected 42, got %d", got)
		}
	})

	t.Run("returns fallback when invalid", func(t *testing.T) {
		t.Setenv("TEST_INVALID_INT", "not_a_number")
		got := getEnvInt("TEST_INVALID_INT", 42)
		if got != 42 {
			t.Errorf("expected 42, got %d", got)
		}
	})

	t.Run("returns parsed int when valid", func(t *testing.T) {
		t.Setenv("TEST_VALID_INT", "100")
		got := getEnvInt("TEST_VALID_INT", 42)
		if got != 100 {
			t.Errorf("expected 100, got %d", got)
		}
	})
}

func TestGetEnvBool(t *testing.T) {
	t.Run("returns fallback when unset", func(t *testing.T) {
		got := getEnvBool("TEST_UNSET_BOOL", true)
		if got != true {
			t.Errorf("expected true, got %v", got)
		}
	})

	t.Run("returns fallback when empty", func(t *testing.T) {
		t.Setenv("TEST_EMPTY_BOOL", "")
		got := getEnvBool("TEST_EMPTY_BOOL", true)
		if got != true {
			t.Errorf("expected true, got %v", got)
		}
	})

	t.Run("returns fallback when invalid", func(t *testing.T) {
		t.Setenv("TEST_INVALID_BOOL", "not_a_bool")
		got := getEnvBool("TEST_INVALID_BOOL", true)
		if got != true {
			t.Errorf("expected true, got %v", got)
		}
	})

	t.Run("returns parsed bool when valid", func(t *testing.T) {
		t.Setenv("TEST_VALID_BOOL", "false")
		got := getEnvBool("TEST_VALID_BOOL", true)
		if got != false {
			t.Errorf("expected false, got %v", got)
		}
	})
}

func TestGetEnvDuration(t *testing.T) {
	t.Run("returns fallback when unset", func(t *testing.T) {
		got := getEnvDuration("TEST_UNSET_DUR", 5*time.Minute)
		if got != 5*time.Minute {
			t.Errorf("expected 5m, got %v", got)
		}
	})

	t.Run("returns fallback when empty", func(t *testing.T) {
		t.Setenv("TEST_EMPTY_DUR", "")
		got := getEnvDuration("TEST_EMPTY_DUR", 5*time.Minute)
		if got != 5*time.Minute {
			t.Errorf("expected 5m, got %v", got)
		}
	})

	t.Run("returns fallback when invalid", func(t *testing.T) {
		t.Setenv("TEST_INVALID_DUR", "invalid_duration")
		got := getEnvDuration("TEST_INVALID_DUR", 5*time.Minute)
		if got != 5*time.Minute {
			t.Errorf("expected 5m, got %v", got)
		}
	})

	t.Run("returns parsed duration when valid", func(t *testing.T) {
		t.Setenv("TEST_VALID_DUR", "12h")
		got := getEnvDuration("TEST_VALID_DUR", 5*time.Minute)
		if got != 12*time.Hour {
			t.Errorf("expected 12h, got %v", got)
		}
	})
}
