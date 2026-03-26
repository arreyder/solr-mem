package main

import "time"

const (
	LifetimePermanent = "permanent"
	LifetimeSession   = "session"
	LifetimeEphemeral = "ephemeral"
	LifetimeTemporary = "temporary"

	EphemeralDuration = 1 * time.Hour
	TemporaryDuration = 7 * 24 * time.Hour
)

// resolveExpiration converts a lifetime value to a concrete expires_at timestamp.
// If expiresAt is explicitly set, it takes precedence over lifetime.
// Returns an empty string for no expiration (permanent/session).
func resolveExpiration(lifetime, expiresAt string) string {
	if expiresAt != "" {
		return expiresAt
	}

	now := time.Now().UTC()
	switch lifetime {
	case LifetimeEphemeral:
		return now.Add(EphemeralDuration).Format(time.RFC3339)
	case LifetimeTemporary:
		return now.Add(TemporaryDuration).Format(time.RFC3339)
	default:
		return ""
	}
}

// normalizeLifetime returns a valid lifetime value, defaulting to permanent.
func normalizeLifetime(lifetime string) string {
	switch lifetime {
	case LifetimePermanent, LifetimeSession, LifetimeEphemeral, LifetimeTemporary:
		return lifetime
	case "":
		return LifetimePermanent
	default:
		return LifetimePermanent
	}
}
