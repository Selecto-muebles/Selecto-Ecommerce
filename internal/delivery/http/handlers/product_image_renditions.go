package handlers

import (
	"container/list"
	"crypto/sha256"
	"fmt"
	"sync"

	"Selecto-Ecommerce/internal/service/editorial"
	"github.com/jackc/pgx/v5/pgxpool"
)

type imageRenditionKey struct {
	pool            *pgxpool.Pool
	id, width, size int
	created         int64
}

type imageRendition struct {
	content    []byte
	mime, etag string
}

type cachedImageRendition struct {
	key   imageRenditionKey
	value imageRendition
}

// Bound both encoded bytes and entry count. Originals stay in SQL; decoded
// bitmaps are never cached. Misses are serialized to bound CPU and memory.
type imageRenditionCache struct {
	mu                          sync.Mutex
	work                        sync.Mutex
	items                       map[imageRenditionKey]*list.Element
	order                       *list.List
	bytes, maxBytes, maxEntries int
}

var productRenditions = imageRenditionCache{maxBytes: 16 << 20, maxEntries: 256}

func (cache *imageRenditionCache) get(key imageRenditionKey) (imageRendition, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if element := cache.items[key]; element != nil {
		cache.order.MoveToFront(element)
		return element.Value.(cachedImageRendition).value, true
	}
	return imageRendition{}, false
}

func (cache *imageRenditionCache) load(key imageRenditionKey, fetch func() ([]byte, string, error)) (imageRendition, error) {
	if value, ok := cache.get(key); ok {
		return value, nil
	}
	cache.work.Lock()
	defer cache.work.Unlock()
	if value, ok := cache.get(key); ok {
		return value, nil
	}
	content, mime, err := fetch()
	if err != nil {
		return imageRendition{}, err
	}
	if variant, err := editorial.PrepareRendition(content, key.width); err == nil {
		content, mime = variant.Content, variant.MIME
	}
	// Unsupported/oversized originals retain their previous serving behavior.
	digest := sha256.Sum256(content)
	value := imageRendition{content, mime, fmt.Sprintf(`"product-%d-%d-%x"`, key.id, key.width, digest[:12])}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(content) <= cache.maxBytes {
		if cache.items == nil {
			cache.items = make(map[imageRenditionKey]*list.Element)
			cache.order = list.New()
		}
		for cache.order.Len() > 0 && (cache.bytes+len(content) > cache.maxBytes || cache.order.Len() >= cache.maxEntries) {
			oldest := cache.order.Back()
			entry := oldest.Value.(cachedImageRendition)
			cache.bytes -= len(entry.value.content)
			delete(cache.items, entry.key)
			cache.order.Remove(oldest)
		}
		cache.items[key] = cache.order.PushFront(cachedImageRendition{key, value})
		cache.bytes += len(content)
	}
	return value, nil
}
