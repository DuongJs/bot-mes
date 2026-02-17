package commands

import (
	"context"
	"testing"
	"time"
)

func TestRegistryExecuteUnknown(t *testing.T) {
	r := NewRegistry()
	err := r.Execute("nonexistent", &Context{
		Ctx:     context.Background(),
		Message: &WrappedMessage{SenderId: 1},
	})
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

func TestRegistryCooldown(t *testing.T) {
	r := NewRegistry()
	r.DefaultCooldown = 100 * time.Millisecond

	r.SetCooldown(42, "ping")

	remaining, inCooldown := r.CheckCooldown(42, "ping")
	if !inCooldown {
		t.Fatal("expected sender 42 to be in cooldown")
	}
	if remaining <= 0 {
		t.Fatalf("expected positive remaining, got %v", remaining)
	}

	// Different sender should not be in cooldown
	_, inCooldown = r.CheckCooldown(99, "ping")
	if inCooldown {
		t.Fatal("sender 99 should not be in cooldown")
	}

	// Different command for same sender should not be in cooldown
	_, inCooldown = r.CheckCooldown(42, "help")
	if inCooldown {
		t.Fatal("sender 42 should not be in cooldown for help")
	}

	// Wait for cooldown to expire
	time.Sleep(150 * time.Millisecond)
	_, inCooldown = r.CheckCooldown(42, "ping")
	if inCooldown {
		t.Fatal("expected cooldown to have expired")
	}
}

func TestRegistryCleanCooldowns(t *testing.T) {
	r := NewRegistry()
	r.DefaultCooldown = 50 * time.Millisecond

	r.SetCooldown(1, "a")
	r.SetCooldown(2, "b")

	time.Sleep(100 * time.Millisecond)

	r.CleanCooldowns()

	r.mu.RLock()
	count := len(r.cooldowns)
	r.mu.RUnlock()

	if count != 0 {
		t.Fatalf("expected 0 cooldowns after cleanup, got %d", count)
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register("ping", &PingCommand{})

	list := r.List()
	if _, ok := list["ping"]; !ok {
		t.Fatal("expected ping in command list")
	}
	if list["ping"] != (&PingCommand{}).Description() {
		t.Fatalf("unexpected description: %s", list["ping"])
	}
}

func TestRegistryCooldownCaseInsensitive(t *testing.T) {
	r := NewRegistry()
	r.DefaultCooldown = 100 * time.Millisecond

	r.SetCooldown(1, "PING")
	_, inCooldown := r.CheckCooldown(1, "ping")
	if !inCooldown {
		t.Fatal("cooldown should be case-insensitive")
	}
}
