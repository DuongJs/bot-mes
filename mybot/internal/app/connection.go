package app

import (
	"context"
	"math/rand/v2"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"

	"mybot/internal/messaging"
)

// connectionLoop runs the main connect→handle→reconnect cycle.
func (b *Bot) connectionLoop(ctx context.Context) {
	for {
		b.botReady.Store(false)
		b.connectOnce(ctx)

		if ctx.Err() != nil {
			return
		}

		select {
		case <-b.fullReconnectCh:
			b.Log.Info().Msg("Full reconnect requested, restarting session...")
		case <-ctx.Done():
			return
		}
	}
}

// connectOnce performs a single connection lifecycle: login → connect → wait.
func (b *Bot) connectOnce(ctx context.Context) {
	// Auto-login if enabled and cookies are missing.
	if b.Cfg.AutoLogin.Enabled && b.Cfg.AutoLogin.UID != "" && b.Cfg.AutoLogin.Password != "" {
		if !hasCookies(b.Cfg.Cookies) {
			b.Log.Info().Msg("No valid cookies found, attempting auto-login...")
			if err := b.doAutoLogin(); err != nil {
				b.Log.Error().Err(err).Msg("Auto-login failed")
			} else {
				b.Log.Info().Msg("Auto-login succeeded, cookies updated")
			}
		}
	}

	newClient := b.buildClient()
	b.setClient(newClient)
	newClient.SetEventHandler(b.handleEvent)

	user, initialTable, err := newClient.LoadMessagesPage(ctx)
	if err != nil {
		// Retry with auto-login if enabled.
		if b.Cfg.AutoLogin.Enabled && b.Cfg.AutoLogin.UID != "" && b.Cfg.AutoLogin.Password != "" {
			b.Log.Warn().Err(err).Msg("LoadMessagesPage failed, attempting auto-login...")
			if loginErr := b.doAutoLogin(); loginErr != nil {
				b.Log.Error().Err(loginErr).Msg("Auto-login failed")
			} else {
				b.Log.Info().Msg("Auto-login succeeded, retrying connection...")
				newClient = b.buildClient()
				b.setClient(newClient)
				newClient.SetEventHandler(b.handleEvent)
				user, initialTable, err = newClient.LoadMessagesPage(ctx)
			}
		}
		if err != nil {
			b.Log.Error().Err(err).Msg("Failed to load messages page, retrying in 30s...")
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
			}
			b.triggerReconnect()
			return
		}
	}

	b.selfID.Store(user.GetFBID())
	b.Log.Info().Int64("id", b.selfID.Load()).Msg("Logged in")

	if err := b.messageAPI.ObserveTable(ctx, initialTable, messaging.MetadataOnly); err != nil {
		b.Log.Warn().Err(err).Msg("Failed to seed startup metadata")
	}

	if err := newClient.Connect(ctx); err != nil {
		b.Log.Error().Err(err).Msg("Failed to connect")
		return
	}

	// Periodic reconnect goroutine (pass parent context for proper cancellation).
	go b.periodicReconnect(ctx)

	// Lightweight token refresh (fb_dtsg / jazoest / LSD) without disconnecting.
	go b.tokenRefreshLoop(ctx, newClient)

	// Block until reconnect or shutdown.
	select {
	case <-b.fullReconnectCh:
		b.Log.Info().Msg("Full reconnect triggered, tearing down current session...")
	case <-ctx.Done():
	}

	if cancel := b.stopPeriodicReconn.Load(); cancel != nil {
		(*cancel)()
	}

	b.clientMu.Lock()
	if b.client != nil {
		b.client.Disconnect()
		b.client = nil
	}
	b.clientMu.Unlock()

	if ctx.Err() == nil {
		b.triggerReconnect()
	}
}

// periodicReconnect triggers a full reconnect on a randomised timer.
// The actual delay is between 1x and 2x the configured interval to
// avoid predictable reconnect patterns.
func (b *Bot) periodicReconnect(parent context.Context) {
	intervalSec := b.Cfg.ForceRefreshIntervalSeconds
	if intervalSec < 0 {
		return
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if oldCancel := b.stopPeriodicReconn.Swap(&cancel); oldCancel != nil {
		(*oldCancel)()
	}

	base := time.Duration(intervalSec) * time.Second
	jitter := time.Duration(rand.Int64N(int64(base)))
	interval := base + jitter
	timer := time.NewTimer(interval)
	defer timer.Stop()

	b.Log.Info().Stringer("interval", interval).Msg("Periodic reconnect loop started")

	select {
	case <-timer.C:
		b.Log.Info().Msg("Periodic reconnect timer fired")
		b.triggerReconnect()
	case <-ctx.Done():
	}
}

// tokenRefreshLoop periodically refreshes fb_dtsg, jazoest and LSD tokens
// without tearing down the socket connection.
func (b *Bot) tokenRefreshLoop(ctx context.Context, client *messagix.Client) {
	intervalSec := b.Cfg.TokenRefreshIntervalSeconds
	if intervalSec < 0 {
		return
	}
	interval := time.Duration(intervalSec) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	b.Log.Info().Stringer("interval", interval).Msg("Token refresh loop started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := client.RefreshConfigs(ctx); err != nil {
				b.Log.Warn().Err(err).Msg("Token refresh failed (will retry next tick)")
			}
		}
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────────

// buildClient creates a new Messagix client from current cookies.
func (b *Bot) buildClient() *messagix.Client {
	c := cookies.NewCookies()
	c.Platform = types.Facebook
	for k, v := range b.Cfg.Cookies {
		if v != "" {
			c.Set(cookies.MetaCookieName(k), v)
		}
	}
	return messagix.NewClient(c, b.Log, &messagix.Config{
		MayConnectToDGW: true,
	})
}

// setClient thread-safely replaces the active Messagix client.
func (b *Bot) setClient(c *messagix.Client) {
	b.clientMu.Lock()
	b.client = c
	b.clientMu.Unlock()
}
