//go:build windows

package orchestrator

import "golang.org/x/sys/windows"

// diskFree returns free and total bytes for the volume holding path.
//
// The appliance is Linux-only; this exists so the backend still builds and tests
// on a Windows development machine.
func diskFree(path string) (free, total uint64, err error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeToCaller, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &freeToCaller, &totalBytes, &totalFree); err != nil {
		return 0, 0, err
	}
	return freeToCaller, totalBytes, nil
}
