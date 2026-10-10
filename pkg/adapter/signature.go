package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type signatureEntry struct {
	sig       string
	expiresAt time.Time
}

var (
	sigCacheLock sync.RWMutex
	sigCache     = make(map[string]signatureEntry)
)

func cacheKey(id, name, args string) []string {
	var keys []string
	if id != "" {
		keys = append(keys, "id:"+id)
	}
	if name != "" {
		h := sha256.Sum256([]byte(name + ":" + args))
		keys = append(keys, "hash:"+hex.EncodeToString(h[:8]))
		keys = append(keys, "name:"+name)
	}
	return keys
}

// SaveThoughtSignature records the thoughtSignature returned by upstream for a tool call
func SaveThoughtSignature(id, name, args, sig string) {
	if sig == "" {
		return
	}
	sigCacheLock.Lock()
	defer sigCacheLock.Unlock()

	now := time.Now()
	// Clean up old entries if cache grows
	if len(sigCache) > 2000 {
		for k, v := range sigCache {
			if now.After(v.expiresAt) {
				delete(sigCache, k)
			}
		}
	}

	entry := signatureEntry{
		sig:       sig,
		expiresAt: now.Add(2 * time.Hour),
	}

	for _, k := range cacheKey(id, name, args) {
		sigCache[k] = entry
	}
}

// GetThoughtSignature retrieves the cached signature by ID, name+args, or name
func GetThoughtSignature(id, name, args string) string {
	sigCacheLock.RLock()
	defer sigCacheLock.RUnlock()

	now := time.Now()
	for _, k := range cacheKey(id, name, args) {
		if entry, ok := sigCache[k]; ok {
			if now.Before(entry.expiresAt) {
				return entry.sig
			}
		}
	}
	return ""
}
