package hook

import (
	log "github.com/Sirupsen/logrus"
)

// DefaultHook default hook
func DefaultHook() {
	f := DefaultFormatter(log.Fields{})
	h := NewGlogHook(f)
	log.AddHook(h)
}
