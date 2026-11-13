package ortt

import (
	"bytes"
	"crypto/hmac"
	"encoding/binary"
	"math"
	"sync"
	"time"
)

// ReplayCache provides a 30-second sliding-window cache for packet Nonces
type ReplayCache struct {
	mu      sync.Mutex
	entries map[uint32]time.Time
}

func NewReplayCache() *ReplayCache {
	c := &ReplayCache{
		entries: make(map[uint32]time.Time),
	}
	// Periodically evict expired entries
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			c.evict()
		}
	}()
	return c
}

func (c *ReplayCache) evict() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for nonce, t := range c.entries {
		if now.Sub(t) > MaxClockSkew {
			delete(c.entries, nonce)
		}
	}
}

// CheckAndAdd checks if a nonce was already seen within the window; returns false if duplicate (replay attack)
func (c *ReplayCache) CheckAndAdd(nonce uint32, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, exists := c.entries[nonce]; exists {
		if now.Sub(t) <= MaxClockSkew {
			return false
		}
	}
	c.entries[nonce] = now
	return true
}

// VerifyPacket parses, authenticates, and validates a received v2 control packet (32 bytes wire length)
func VerifyPacket(secret []byte, raw []byte, cache *ReplayCache) (byte, uint32, uint16, error) {
	keyRing := make(map[uint16][]byte)
	if len(secret) > 0 {
		keyRing[1] = secret
	}
	cmd, ts, keyID, _, err := VerifyPacketWithKeys(keyRing, secret, raw, cache)
	return cmd, ts, keyID, err
}

// VerifyPacketWithKeys authenticates raw packet against keyRing and returns the matched secret bytes.
// Security invariant:
//   - KeyID >= 2: STRICT per-node isolation — ONLY keyRing[keyID] is consulted; NEVER falls back to
//     defaultSecret or other keys in keyRing so rotated/disabled/deleted nodes fail immediately.
//   - KeyID <= 1: legacy global slot — checks keyRing[keyID] and falls back to defaultSecret / keyRing[1].
func VerifyPacketWithKeys(keyRing map[uint16][]byte, defaultSecret []byte, raw []byte, cache *ReplayCache) (byte, uint32, uint16, []byte, error) {
	if len(raw) < PacketWireLength {
		return 0, 0, 0, nil, ErrPacketTooShort
	}

	// 1. Magic header check (accepts "ORTT" or "SMTX")
	if !bytes.Equal(raw[:4], Magic) && !bytes.Equal(raw[:4], LegacyMagic) {
		return 0, 0, 0, nil, ErrInvalidMagic
	}

	// 2. Version check (1 byte)
	if raw[4] != ProtocolVersion {
		return 0, 0, 0, nil, ErrInvalidVersion
	}

	cmd := raw[5]
	ts := binary.BigEndian.Uint32(raw[6:10])
	nonce := binary.BigEndian.Uint32(raw[10:14])
	keyID := binary.BigEndian.Uint16(raw[14:16])

	// 3. Timestamp anti-replay check (within MaxClockSkew, 30s)
	now := uint32(time.Now().Unix())
	diff := int64(now) - int64(ts)
	if math.Abs(float64(diff)) > MaxClockSkew.Seconds() {
		return 0, 0, 0, nil, ErrClockSkew
	}

	// 4. HMAC-SHA256 signature verification BEFORE recording nonce so forged packets cannot poison ReplayCache.
	actualSig := raw[16:32]
	var matchedKey []byte

	if keyID >= 2 {
		// Strict per-node KeyID isolation: never fall back to defaultSecret or other KeyIDs.
		if keyRing != nil {
			if candidate := keyRing[keyID]; len(candidate) > 0 {
				expectedSig := ComputeHMAC(candidate, raw[:16])
				if hmac.Equal(expectedSig, actualSig) {
					matchedKey = candidate
				}
			}
		}
	} else {
		// Legacy global slot (KeyID 0 or 1): only allow global KeyID 0/1 and defaultSecret.
		if keyRing != nil {
			if candidate := keyRing[keyID]; len(candidate) > 0 {
				expectedSig := ComputeHMAC(candidate, raw[:16])
				if hmac.Equal(expectedSig, actualSig) {
					matchedKey = candidate
				}
			}
		}
		if matchedKey == nil && len(defaultSecret) > 0 {
			expectedSig := ComputeHMAC(defaultSecret, raw[:16])
			if hmac.Equal(expectedSig, actualSig) {
				matchedKey = defaultSecret
			}
		}
		if matchedKey == nil && keyID == 0 && keyRing != nil {
			if candidate := keyRing[DefaultKeyID]; len(candidate) > 0 {
				expectedSig := ComputeHMAC(candidate, raw[:16])
				if hmac.Equal(expectedSig, actualSig) {
					matchedKey = candidate
				}
			}
		}
	}

	if matchedKey == nil {
		return 0, 0, 0, nil, ErrHMACMismatch
	}

	// 5. Nonce sliding-window uniqueness check
	if cache != nil {
		if !cache.CheckAndAdd(nonce, time.Now()) {
			return 0, 0, 0, nil, ErrReplayDetected
		}
	}

	secretCopy := make([]byte, len(matchedKey))
	copy(secretCopy, matchedKey)
	return cmd, ts, keyID, secretCopy, nil
}
