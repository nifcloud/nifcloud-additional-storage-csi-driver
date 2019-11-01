package devicemanager

import (
	"fmt"
)

// ExistingNames is a map of assigned device names. Presence of a key with a device
// name in the map means that the device is allocated. Value is irrelevant and
// can be used for anything that NameAllocator user wants.  Only the relevant
// part of device name should be in the map, e.g. "b" for "/dev/sdb".
type ExistingNames map[string]string

// NameAllocator finds available device name, taking into account already
// assigned device names from ExistingNames map. It tries to find the next
// device name to the previously assigned one (from previous NameAllocator
// call), so all available device names are used eventually and it minimizes
// device name reuse.
type NameAllocator interface {
	// GetNext returns a free device name or error when there is no free device
	// name. Only the device name is returned, e.g. "b" for "/dev/sdb".
	GetNext(existingNames ExistingNames) (name string, err error)
}

type nameAllocator struct{}

var _ NameAllocator = &nameAllocator{}

// GetNext gets next available device given existing names that are being used
// This function iterate through the device names in deterministic order of:
//     b,c ... z
// and return the first one that is not used yet.
func (d *nameAllocator) GetNext(existingNames ExistingNames) (string, error) {
	for c := 1; c <= 15; c++ {
		// FIXME: SCSI controller number 0 is hard coded
		name := fmt.Sprintf("SCSI (0:%d)", c)
		if _, found := existingNames[name]; !found {
			return name, nil
		}
	}

	return "", fmt.Errorf("there are no names available")
}
