package isureal

import (
	"container/list"
	"os"
	"strconv"
	"sync"
)

// image_objects は INSERT / DELETE のみで中身が変わらないので、本体はキャッシュしてよい。
// 返してよいかどうか（生存確認）は毎回 DB で確認する。ここでは持たない。
// メモリに無ければローカルディスク（image_disk.go）を見る。put はディスクにも書く。
// 既定 1GB。ISUREAL_IMAGE_CACHE_MB（env.sh）で上書きできる（#258。asia1 は imgcache base がディスクにあるので縮めてよい）
var imageCacheMaxBytes = imageCacheMaxBytesFromEnv()

func imageCacheMaxBytesFromEnv() int {
	if mb, err := strconv.Atoi(os.Getenv("ISUREAL_IMAGE_CACHE_MB")); err == nil && mb > 0 {
		return mb << 20
	}
	return 1 << 30
}

type imageCacheEntry struct {
	id        string
	mediaType string
	data      []byte
}

type imageCache struct {
	mu    sync.Mutex
	bytes int
	ll    *list.List
	items map[string]*list.Element
}

var imageObjectCache = &imageCache{ll: list.New(), items: map[string]*list.Element{}}

func (c *imageCache) get(id string) (imageCacheEntry, bool) {
	c.mu.Lock()
	el, ok := c.items[id]
	if ok {
		c.ll.MoveToFront(el)
		entry := *el.Value.(*imageCacheEntry)
		c.mu.Unlock()
		return entry, true
	}
	c.mu.Unlock()
	data, ok := imageDiskGet(id)
	if !ok {
		return imageCacheEntry{}, false
	}
	c.putMem(id, "image/jpeg", data)
	return imageCacheEntry{id: id, mediaType: "image/jpeg", data: data}, true
}

func (c *imageCache) put(id, mediaType string, data []byte) {
	imageDiskPut("run", id, mediaType, data)
	c.putMem(id, mediaType, data)
}

func (c *imageCache) putMem(id, mediaType string, data []byte) {
	if len(data) > imageCacheMaxBytes/4 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[id]; ok {
		c.ll.MoveToFront(el)
		return
	}
	c.items[id] = c.ll.PushFront(&imageCacheEntry{id: id, mediaType: mediaType, data: data})
	c.bytes += len(data)
	for c.bytes > imageCacheMaxBytes {
		el := c.ll.Back()
		e := el.Value.(*imageCacheEntry)
		c.ll.Remove(el)
		delete(c.items, e.id)
		c.bytes -= len(e.data)
	}
}

func (c *imageCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bytes = 0
	c.ll.Init()
	c.items = map[string]*list.Element{}
}
