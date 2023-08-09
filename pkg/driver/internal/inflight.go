// This code was forked from https://github.com/kubernetes-sigs/aws-ebs-csi-driver/blob/v1.21.0/pkg/driver/internal/inflight.go

package internal

import (
	"sync"

	"k8s.io/klog/v2"
)

// Idempotent is the interface required to manage in flight requests.
type Idempotent interface {
	// The CSI data types are generated using a protobuf.
	// The generated structures are guaranteed to implement the Stringer interface.
	// Example: https://github.com/container-storage-interface/spec/blob/master/lib/go/csi/csi.pb.go#L3508
	// We can use the generated string as the key of our internal inflight database of requests.
	String() string
}

const (
	VolumeOperationAlreadyExistsErrorMsg = "An operation with the given Volume %s already exists"
)

// InFlight is a struct used to manage in flight requests per volumeId.
type InFlight struct {
	mux      *sync.Mutex
	inFlight map[string]bool
}

// NewInFlight instanciates a InFlight structures.
func NewInFlight() *InFlight {
	return &InFlight{
		mux:      &sync.Mutex{},
		inFlight: make(map[string]bool),
	}
}

// Insert inserts the entry to the current list of inflight request key is volumeId for node and req hash for controller .
// Returns false when the key already exists.
func (db *InFlight) Insert(key string) bool {
	db.mux.Lock()
	defer db.mux.Unlock()

	_, ok := db.inFlight[key]
	if ok {
		return false
	}

	db.inFlight[key] = true
	return true
}

// Delete removes the entry from the inFlight entries map.
// It doesn't return anything, and will do nothing if the specified key doesn't exist.
func (db *InFlight) Delete(key string) {
	db.mux.Lock()
	defer db.mux.Unlock()

	delete(db.inFlight, key)
	klog.V(4).InfoS("Node Service: volume operation finished", "key", key)
}
