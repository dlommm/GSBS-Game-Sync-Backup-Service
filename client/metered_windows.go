//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	iphlpapi            = syscall.NewLazyDLL("iphlpapi.dll")
	getConnectivityHint = iphlpapi.NewProc("GetNetworkConnectivityHint")
)

// NL_NETWORK_CONNECTIVITY_COST_HINT values (nldef.h).
const (
	nlCostUnrestricted = 0
	nlCostFixed        = 1
	nlCostVariable     = 2 // metered / variable cost
)

// IsMeteredConnection returns true if Windows reports the current network as metered (variable cost).
// Requires Windows 10 2004+. Returns false on older Windows or if the API is unavailable.
func IsMeteredConnection() bool {
	if err := getConnectivityHint.Find(); err != nil {
		return false
	}
	// NL_NETWORK_CONNECTIVITY_HINT: ConnectivityLevel (4), ConnectivityCost (4), ApproachingDataLimit (1), OverDataLimit (1), Roaming (1)
	var hint [16]byte
	ret, _, _ := getConnectivityHint.Call(uintptr(unsafe.Pointer(&hint[0])))
	if ret != 0 {
		return false
	}
	// ConnectivityCost is the second uint32 (offset 4).
	cost := *(*uint32)(unsafe.Pointer(&hint[4]))
	return cost == nlCostVariable
}
