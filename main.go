package main

/*
#cgo CFLAGS: -Iheaders
#cgo LDFLAGS: -L/usr/lib/jabra -ljabra

#include "Common.h"
#include "GoWrapper.h"
#include <stdlib.h>
*/
import "C"
import (
	"log"
	"unsafe"

	"fyne.io/systray"
)

func main() {
	appId := C.CString("JabraLink")
	defer C.free(unsafe.Pointer(appId))
	C.Jabra_SetAppID(appId)

	// Callback parameters: FirstScanForDevicesDoneFunc, DeviceAttachedFunc, DeviceRemovedFunc,
	// ButtonInDataRawHidFunc, ButtonInDataTranslatedFunc, nonJabraDeviceDetection, configParams
	if ok := C.Jabra_InitializeV2(
		nil,                              // FirstScanForDevicesDoneFunc (not used here)
		(*[0]byte)(C.deviceAttachedFunc), // Callback for when a device is attached
		(*[0]byte)(C.deviceRemovedFunc),  // Callback for when a device is removed
		nil,                              // Callback for raw HID button input (not used here)
		nil,                              // Callback for translated button input (not used here)
		false,                            // nonJabraDeviceDetection (not used here)
		nil,                              // Additional configuration parameters (not used here)
	); !ok {
		log.Fatalln("Failed to initialize Jabra SDK")
	}
	defer uninitialize()

	systray.Run(onTrayReady, onTrayExit)
}
