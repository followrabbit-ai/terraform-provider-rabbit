// Package mutex provides a keyed mutex used by the group_member resource to
// serialise concurrent read-modify-write apply operations against the same
// Rabbit group. Modelled after hashicorp/terraform-plugin-sdk's mutexkv.
package mutex

import (
	"sync"
)

// KV is a mutex keyed by string.
type KV struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// New returns a ready-to-use KV.
func New() *KV {
	return &KV{locks: make(map[string]*sync.Mutex)}
}

// Lock acquires the named mutex, allocating it on first use.
func (k *KV) Lock(key string) {
	k.get(key).Lock()
}

// Unlock releases the named mutex. It is a programmer error to Unlock a key
// that was never Locked.
func (k *KV) Unlock(key string) {
	k.get(key).Unlock()
}

func (k *KV) get(key string) *sync.Mutex {
	k.mu.Lock()
	defer k.mu.Unlock()
	m, ok := k.locks[key]
	if !ok {
		m = &sync.Mutex{}
		k.locks[key] = m
	}
	return m
}
