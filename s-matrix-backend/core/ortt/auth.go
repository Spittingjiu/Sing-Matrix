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
	return VerifyPacketWithKeys(keyRing, secret, raw, cache)
}

// VerifyPacketWithKeys authenticates raw packet against key ring, primary secret, or fallback
func VerifyPacketWithKeys(keyRing map[uint16][]byte, defaultSecret []byte, raw []byte, cache *ReplayCache) (byte, uint32, uint16, error) {
	if len(raw) < PacketWireLength {
		return 0, 0, 0, ErrPacketTooShort
	}

	// 1. Magic header check (accepts "ORTT" or "SMTX")
	if !bytes.Equal(raw[:4], Magic) && !bytes.Equal(raw[:4], LegacyMagic) {
		return 0, 0, 0, ErrInvalidMagic
	}

	// 2. Version check (1 byte)
	if raw[4] != ProtocolVersion {
		return 0, 0, 0, ErrInvalidVersion
	}

	cmd := raw[5]
	ts := binary.BigEndian.Uint32(raw[6:10])
	nonce := binary.BigEndian.Uint32(raw[10:14])
	keyID := binary.BigEndian.Uint16(raw[14:16])

	// 3. Timestamp anti-replay check (within MaxClockSkew, 30s)
	now := uint32(time.Now().Unix())
	diff := int64(now) - int64(ts)
	if math.Abs(float64(diff)) > MaxClockSkew.Seconds() {
		return 0, 0, 0, ErrClockSkew
	}

	// 4. Nonce sliding-window uniqueness check
	if cache != nil {
		if !cache.CheckAndAdd(nonce, time.Now()) {
			return 0, 0, 0, ErrReplayDetected
		}
	}

	// 5. HMAC-SHA256 signature verification with multi-key fallback
	actualSig := raw[16:32]
	var matchedKey []byte

	// 5.1 Try KeyRing by KeyID
	if keyRing != nil && keyRing[keyID] != nil {
		expectedSig := ComputeHMAC(keyRing[keyID], raw[:16])
		if hmac.Equal(expectedSig, actualSig) {
			matchedKey = keyRing[keyID]
		}
	}

	// 5.2 Try defaultSecret if not matched yet
	if matchedKey == nil && len(defaultSecret) > 0 {
		expectedSig := ComputeHMAC(defaultSecret, raw[:16])
		if hmac.Equal(expectedSig, actualSig) {
			matchedKey = defaultSecret
		}
	}

	// 5.3 Fallback: iterate remaining keys in keyRing
	if matchedKey == nil && keyRing != nil {
		for _, candidate := range keyRing {
			expectedSig := ComputeHMAC(candidate, raw[:16])
			if hmac.Equal(expectedSig, actualSig) {
				matchedKey = candidate
				break
			}
		}
	}



	if matchedKey == nil {
		return 0, 0, 0, ErrHMACMismatch
	}

	return cmd, ts, keyID, nil
}
