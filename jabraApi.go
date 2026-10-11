package main

/*
#cgo CFLAGS: -Iheaders
#cgo LDFLAGS: -L/usr/lib/jabra -ljabra

#include "Common.h"
#include "JabraDeviceConfig.h"
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"
)

type jabra_DeviceInfo struct {
	deviceID               uint16
	productID              uint16
	vendorID               uint16
	deviceName             string
	usbDevicePath          string
	parentInstanceId       string
	errStatus              errorStatusCode
	isDongle               bool
	dongleName             string
	variant                string
	serialNumber           string
	isInFirmwareUpdateMode bool
	deviceConnection       deviceConnectionType
	connectionID           uint32
	parentDeviceID         uint16
	deviceEventsMask       uint32
	featureFlags           *featureFlags
	batteryStatus          *batteryStatus
	pairingList            *pairingList
}

type batteryComponent int

const (
	unknown       batteryComponent = iota // Unable to determine the component.
	headband                              // Generally applies to headsets with headband that only contains one battery.
	combinde                              // For headsets that contains multiple batteries but is not capable of sending each individual state.
	right                                 // The battery in the right unit
	left                                  // The battery in the left unit
	cradleBattery                         // The battery in the cradle
	remoteControl                         // The battery in the remote control
)

type batteryStatusUnit struct {
	levelInPercent uint8
	component      batteryComponent
}

type batteryStatus struct {
	levelInPercent  uint8
	charging        bool
	batteryLow      bool
	component       batteryComponent
	extraUnitsCount uint32 // count of extra units
	extraUnits      []batteryStatusUnit
}

type deviceListType int

const (
	searchResult deviceListType = iota
	pairedDevices
	searchComplete
)

type pairedDevice struct {
	deviceName   string
	deviceBTAddr [6]byte
	isConnected  bool
}

type pairingList struct {
	count         uint16
	listType      deviceListType
	pairedDevices []pairedDevice
}

type secureConnectionMode int

const (
	legacyMode     secureConnectionMode = iota // Normal pairing allowed
	secureMode                                 // Device is allowed to connect a audio gateway eg. a mobile phone
	restrictedMode                             // Pairing not allowed
)

type featureFlags struct {
	busyLight                          bool
	factoryReset                       bool
	pairingList                        bool
	remoteMMI                          bool
	musicEqualizer                     bool
	earbudInterconnectionStatus        bool
	stepRate                           bool
	heartRate                          bool
	rrInterval                         bool
	ringtoneUpload                     bool
	imageUpload                        bool
	needsExplicitRebootAfterOta        bool
	needsToBePutIncCradleToCompleteFwu bool
	remoteMMIv2                        bool
	logging                            bool
	preferredSoftphoneListInDevice     bool
	voiceAssistant                     bool
	playRingtone                       bool
	setDateTime                        bool
	fullWizardMode                     bool
	limitedWizardMode                  bool
	onHeadDetection                    bool
	settingsChangeNotification         bool
	audioStreaming                     bool
	customerSupport                    bool
	mySound                            bool
	uiConfigurableButtons              bool
	manualBusyLight                    bool
	whiteboard                         bool
	video                              bool
	ambienceModes                      bool
	sealingTest                        bool
	amasupport                         bool
	ambienceModesLoop                  bool
	ffanc                              bool
	googleBisto                        bool
	virtualDirector                    bool
	pictureInPicture                   bool
	dateTimeIsUTC                      bool
	remoteControl                      bool
	userConfigurableHdr                bool
	dectBasicPairing                   bool
	dectSecurePairing                  bool
	dectOtaFwuSupported                bool
	xpressURL                          bool
	passwordProvisioning               bool
	ethernet                           bool
	wlan                               bool
	ethernetAuthenticationCertificate  bool
	ethernetAuthenticationMschapv2     bool
	wlanAuthenticationCertificate      bool
	wlanAuthenticationMschapv2         bool
}

type hidInput int

const (
	undefined hidInput = iota
	offHook
	mute
	flash
	redial
	key0
	key1
	key2
	key3
	key4
	key5
	key6
	key7
	key8
	key9
	keyStar
	keyPound
	keyClear
	online
	speedDial
	voiceMail
	lineBusy
	rejectCall
	outOfRange
	pseudoOffHook
	button1
	button2
	button3
	volumeUp
	volumeDown
	fireAlarm
	jackConnection
	qdConnection
	headsetConnection
)

type devices map[int]*jabra_DeviceInfo
type deviceConnectionType int

const (
	deviceConnectionType_USB deviceConnectionType = iota
	deviceConnectionType_BT
	deviceConnectionType_DECT
)

var (
	// stateMu guards deviceManager, selectedHeadset, selectedDongle and the
	// mutable fields hanging off the *jabra_DeviceInfo values stored in it
	// (pairingList, batteryStatus). Anything reading or writing that shared
	// state -- including the C SDK calls that refresh it -- must hold this
	// lock. Long-running SDK calls (e.g. the ~20s device search) must copy
	// out what they need and release the lock before making the call.
	stateMu sync.Mutex

	// deviceManager
	deviceManager   devices
	selectedHeadset int = -1
	selectedDongle  int = -1

	// holding all the new devide found on BT
	searchDeviceList *pairingList = &pairingList{
		count:         0,
		listType:      searchResult,
		pairedDevices: make([]pairedDevice, 0),
	}

	// Stop Channels
	stopUpdateBattery     = make(chan struct{})
	stopUpdatePairingList = make(chan struct{})

	// stateChangeCh receives a (non-blocking) notification whenever
	// deviceManager changes shape, so the tray menu can rebuild immediately
	// instead of waiting for its periodic refresh tick.
	stateChangeCh = make(chan struct{}, 1)
)

func notifyStateChanged() {
	select {
	case stateChangeCh <- struct{}{}:
	default:
	}
}

/****************************************************************************/
/*                             C CALLBACKS	                                */
/****************************************************************************/

// Reminder: If you plan to use this function, make sure to update the `goWrapper.h` file accordingly.
// //export firstScanForDevicesDone
// func firstScanForDevicesDone() {
// 	 fmt.Println("First scan for devices done!")
// }

//export deviceAttachedFunc
func deviceAttachedFunc(deviceInfo C.Jabra_DeviceInfo) {

	goDeviceInfo := &jabra_DeviceInfo{
		deviceID:               uint16(deviceInfo.deviceID),
		productID:              uint16(deviceInfo.productID),
		vendorID:               uint16(deviceInfo.vendorID),
		deviceName:             C.GoString(deviceInfo.deviceName),
		usbDevicePath:          C.GoString(deviceInfo.usbDevicePath),
		parentInstanceId:       C.GoString(deviceInfo.parentInstanceId),
		errStatus:              errorStatusCode(deviceInfo.errStatus),
		isDongle:               bool(deviceInfo.isDongle),
		dongleName:             C.GoString(deviceInfo.dongleName),
		variant:                C.GoString(deviceInfo.variant),
		serialNumber:           C.GoString(deviceInfo.serialNumber),
		isInFirmwareUpdateMode: bool(deviceInfo.isInFirmwareUpdateMode),
		deviceConnection:       deviceConnectionType(deviceInfo.deviceconnection),
		connectionID:           uint32(deviceInfo.connectionId),
		parentDeviceID:         uint16(deviceInfo.parentDeviceId),
	}
	goDeviceInfo.deviceEventsMask = getDeviceEventsMask(goDeviceInfo.deviceID)
	goDeviceInfo.featureFlags = getSupportedFeature(goDeviceInfo.deviceID)

	if !goDeviceInfo.isDongle {
		battery, err := getBatteryStatus(goDeviceInfo.deviceID)
		if err != nil {
			fmt.Printf("Get Battery Status for %s: %s\n", goDeviceInfo.deviceName, err)
		} else {
			goDeviceInfo.batteryStatus = battery
		}
	} else {
		if goDeviceInfo.featureFlags.pairingList {
			goDeviceInfo.pairingList = getPairingList(goDeviceInfo.deviceID)
		}
	}

	if isNewDevice := serialNumberCheck(goDeviceInfo); isNewDevice {
		deviceManager.add(goDeviceInfo)
	}
	C.Jabra_FreeDeviceInfo(deviceInfo)
}

//export deviceRemovedFunc
func deviceRemovedFunc(deviceID uint16) {
	deviceManager.removed(deviceID)
}

// //export buttonInDataRawHidFunc
// func buttonInDataRawHidFunc(deviceid uint16, usagepage uint16, usage uint16, buttonindata bool) {
// 	// log.Println("RawHid", deviceid, usagepage, usage, buttonindata)

// }

// //export buttonInDataTranslatedFunc
// func buttonInDataTranslatedFunc(deviceid uint16, jabra_HidInput hidInput, buttonindata bool) {
// 	// log.Println("Translated", deviceid, jabra_HidInput, buttonindata)
// 	// if jabra_HidInput == headsetConnection {
// 	// 	fmt.Println("headsetConnection")
// 	// }
// }

func updatePairingList() {

	for {
		select {
		case <-stopUpdatePairingList:
			return
		default:
			stateMu.Lock()
			if dongle, exists := deviceManager[selectedDongle]; exists && dongle.pairingList != nil {
				updated := getPairingList(dongle.deviceID)
				dongle.pairingList.count = updated.count
				dongle.pairingList.listType = updated.listType
				dongle.pairingList.pairedDevices = updated.pairedDevices
			}
			stateMu.Unlock()
			notifyStateChanged()
			time.Sleep(time.Second)
		}
	}

}

func batteryStatusUpdate() {
	for {
		select {
		case <-stopUpdateBattery:
			return
		default:
			stateMu.Lock()
			if device, exists := deviceManager[selectedHeadset]; exists {
				battery, err := getBatteryStatus(device.deviceID)
				if err != nil {
					fmt.Println("Error getBatteryStatus")
				} else {
					// Note: The battery percentage increases by a certain amount when charging (e.g., from 83% to 90%).
					// The exact reason for this behavior is unclear but might be related to factors like the battery's charge cycle or charging efficiency.
					device.batteryStatus.levelInPercent = battery.levelInPercent
					device.batteryStatus.charging = battery.charging
					device.batteryStatus.batteryLow = battery.batteryLow
					device.batteryStatus.component = battery.component
					device.batteryStatus.extraUnitsCount = battery.extraUnitsCount
					device.batteryStatus.extraUnits = battery.extraUnits
				}
			}
			stateMu.Unlock()
			time.Sleep(time.Second)
		}
	}

}

// Reminder: If you plan to use this function, make sure to update the `goWrapper.h` file accordingly.
// The current callback behavior is inconsistent. While the charging status updates as expected,
// the `levelInPercent` callback is sometimes delayed.
// //export batteryStatusUpdate
// func batteryStatusUpdate(deviceID uint16, cBatteryStatus *C.Jabra_BatteryStatus) {

// 	for _, device := range deviceManager {
// 		if device.deviceID == deviceID {
// 			// A shallow copy is not used here because this can be slower due to the extra allocation,
// 			// and modifying the pointer directly is faster than copying the entire structure.
// 			device.batteryStatus.levelInPercent = uint8(cBatteryStatus.levelInPercent)
// 			device.batteryStatus.charging = bool(cBatteryStatus.charging)
// 			device.batteryStatus.batteryLow = bool(cBatteryStatus.batteryLow)
// 			device.batteryStatus.component = batteryComponent(cBatteryStatus.component)
// 			device.batteryStatus.extraUnitsCount = uint32(cBatteryStatus.extraUnitsCount)

// 			if cBatteryStatus.extraUnitsCount > 0 && cBatteryStatus.extraUnits != nil {
// 				clear(device.batteryStatus.extraUnits)
// 				extraUnits := (*[1 << 30]C.Jabra_BatteryStatusUnit)(unsafe.Pointer(cBatteryStatus.extraUnits))[:cBatteryStatus.extraUnitsCount:cBatteryStatus.extraUnitsCount]
// 				for _, unit := range extraUnits {
// 					device.batteryStatus.extraUnits = append(device.batteryStatus.extraUnits, batteryStatusUnit{
// 						levelInPercent: uint8(unit.levelInPercent),
// 						component:      batteryComponent(unit.component),
// 					})
// 				}
// 			}
// 		}
// 	}

// 	C.Jabra_FreeBatteryStatus(cBatteryStatus)
// }

/****************************************************************************/
/*                           GENERAL UTILITES                               */
/****************************************************************************/

func serialNumberCheck(deviceInfo *jabra_DeviceInfo) bool {
	if deviceInfo.serialNumber == "" {
		return false
	}

	stateMu.Lock()
	defer stateMu.Unlock()

	var isNewDevice bool

	if deviceInfo.isDongle {
		if dongle, exists := deviceManager[selectedDongle]; exists {
			if dongle.serialNumber == deviceInfo.serialNumber && dongle.deviceConnection == deviceInfo.deviceConnection {
				dongle.deviceID = deviceInfo.deviceID
				isNewDevice = false
			} else {
				isNewDevice = true
			}
		} else {
			if selectedDongle == -1 {
				isNewDevice = true
			} else {
				panic("what is going on")
			}
		}
	} else {

		if device, exists := deviceManager[selectedHeadset]; exists {
			if device.serialNumber == deviceInfo.serialNumber && device.deviceConnection == deviceInfo.deviceConnection {
				device.deviceID = deviceInfo.deviceID
				isNewDevice = false
			} else {

				isNewDevice = true

			}
		} else {
			if selectedHeadset == -1 {
				isNewDevice = true
			} else {
				panic("what is going on")
			}
		}

	}

	return isNewDevice
}

func (d *devices) add(deviceInfo *jabra_DeviceInfo) {
	stateMu.Lock()

	if *d == nil {
		*d = make(map[int]*jabra_DeviceInfo)
	}

	id := len(*d)
	startPairingPoll := false
	startBatteryPoll := false
	if deviceInfo.isDongle {
		if selectedDongle == -1 {
			selectedDongle = id
			startPairingPoll = true
		}
	} else {
		if selectedHeadset == -1 {
			selectedHeadset = id
			startBatteryPoll = true
		}
	}

	(*d)[id] = deviceInfo
	stateMu.Unlock()

	if startPairingPoll {
		go updatePairingList()
	}
	if startBatteryPoll {
		go batteryStatusUpdate()
	}

	notifyStateChanged()
}

func (d *devices) removed(deviceID uint16) {
	stateMu.Lock()

	if *d == nil {
		stateMu.Unlock()
		return
	}

	var (
		checkDongleExists  bool
		checkHeadSetExists bool
	)

	newDevices := make(map[int]*jabra_DeviceInfo)

	nextIndex := 0
	for i := 0; i < len(*d); i++ {
		device, exists := (*d)[i]
		if !exists || device.deviceID == deviceID {
			continue
		}

		if device.isDongle {
			checkDongleExists = true
			selectedDongle = nextIndex
		} else {
			checkHeadSetExists = true
			selectedHeadset = nextIndex
		}

		newDevices[nextIndex] = device
		nextIndex++
	}
	if !checkDongleExists {
		selectedDongle = -1
	}
	if !checkHeadSetExists {
		selectedHeadset = -1
	}

	*d = newDevices
	stateMu.Unlock()

	// Sent after unlocking: the polling goroutines acquire stateMu on every
	// iteration, so signaling them to stop while still holding the lock here
	// could deadlock against a goroutine currently waiting to acquire it.
	if !checkDongleExists {
		stopUpdatePairingList <- struct{}{}
	}
	if !checkHeadSetExists {
		stopUpdateBattery <- struct{}{}
	}

	notifyStateChanged()
}

func uninitialize() {
	if uninit := C.Jabra_Uninitialize(); !uninit {
		fmt.Println("Failed Uninitialize")
	}
}

func factoryReset(deviceID uint16) error {
	if err := returnCode(int(C.Jabra_FactoryReset(C.ushort(deviceID)))); err != nil {
		return err
	}
	return nil
}

func getJabraSdkVersion() string {
	const bufferSize = 16

	buffer := make([]byte, bufferSize)
	cBuffer := (*C.char)(unsafe.Pointer(&buffer[0]))
	err := returnCode(int(C.Jabra_GetVersion(cBuffer, C.int(bufferSize))))
	if err != nil {
		log.Fatalln(err)
	}

	return C.GoString(cBuffer)
}

func getSupportedFeature(deviceID uint16) *featureFlags {

	var featureFlag featureFlags
	var count C.uint32_t

	cFeatures := C.Jabra_GetSupportedFeatures(C.ushort(deviceID), &count)
	if cFeatures == nil {
		return &featureFlag
	}
	features := (*[1 << 30]uint32)(unsafe.Pointer(cFeatures))[:count:count]

	for _, cFeature := range features {
		switch cFeature {
		case 1000:
			featureFlag.busyLight = true
		case 1001:
			featureFlag.factoryReset = true
		case 1002:
			featureFlag.pairingList = true
		case 1003:
			featureFlag.remoteMMI = true
		case 1004:
			featureFlag.musicEqualizer = true
		case 1005:
			featureFlag.earbudInterconnectionStatus = true
		case 1006:
			featureFlag.stepRate = true
		case 1007:
			featureFlag.heartRate = true
		case 1008:
			featureFlag.rrInterval = true
		case 1009:
			featureFlag.ringtoneUpload = true
		case 1010:
			featureFlag.imageUpload = true
		case 1011:
			featureFlag.needsExplicitRebootAfterOta = true
		case 1012:
			featureFlag.needsToBePutIncCradleToCompleteFwu = true
		case 1013:
			featureFlag.remoteMMIv2 = true
		case 1014:
			featureFlag.logging = true
		case 1015:
			featureFlag.preferredSoftphoneListInDevice = true
		case 1016:
			featureFlag.voiceAssistant = true
		case 1017:
			featureFlag.playRingtone = true
		case 1018:
			featureFlag.setDateTime = true
		case 1019:
			featureFlag.fullWizardMode = true
		case 1020:
			featureFlag.limitedWizardMode = true
		case 1021:
			featureFlag.onHeadDetection = true
		case 1022:
			featureFlag.settingsChangeNotification = true
		case 1023:
			featureFlag.audioStreaming = true
		case 1024:
			featureFlag.customerSupport = true
		case 1025:
			featureFlag.mySound = true
		case 1026:
			featureFlag.uiConfigurableButtons = true
		case 1027:
			featureFlag.manualBusyLight = true
		case 1028:
			featureFlag.whiteboard = true
		case 1029:
			featureFlag.video = true
		case 1030:
			featureFlag.ambienceModes = true
		case 1031:
			featureFlag.sealingTest = true
		case 1032:
			featureFlag.amasupport = true
		case 1033:
			featureFlag.ambienceModesLoop = true
		case 1034:
			featureFlag.ffanc = true
		case 1035:
			featureFlag.googleBisto = true
		case 1036:
			featureFlag.virtualDirector = true
		case 1037:
			featureFlag.pictureInPicture = true
		case 1038:
			featureFlag.dateTimeIsUTC = true
		case 1039:
			featureFlag.remoteControl = true
		case 1040:
			featureFlag.userConfigurableHdr = true
		case 1041:
			featureFlag.dectBasicPairing = true
		case 1042:
			featureFlag.dectSecurePairing = true
		case 1043:
			featureFlag.dectOtaFwuSupported = true
		case 1044:
			featureFlag.xpressURL = true
		case 1045:
			featureFlag.passwordProvisioning = true
		case 1046:
			featureFlag.ethernet = true
		case 1047:
			featureFlag.wlan = true
		case 1048:
			featureFlag.ethernetAuthenticationCertificate = true
		case 1049:
			featureFlag.ethernetAuthenticationMschapv2 = true
		case 1050:
			featureFlag.wlanAuthenticationCertificate = true
		case 1051:
			featureFlag.wlanAuthenticationMschapv2 = true
		}
	}
	// Free features array
	C.Jabra_FreeSupportedFeatures(cFeatures)

	return &featureFlag
}

func getDeviceEventsMask(deviceID uint16) uint32 {
	return uint32(C.Jabra_GetSupportedDeviceEvents(C.ushort(deviceID)))
}

/****************************************************************************/
/*                               BLUETOOTH                                  */
/****************************************************************************/

// dongleDeviceID returns the currently attached dongle's SDK deviceID.
// It only holds stateMu long enough to copy the value out, so callers are
// free to follow it with a slow (or blocking) SDK call.
func dongleDeviceID() (uint16, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	dongle, exists := deviceManager[selectedDongle]
	if !exists {
		return 0, false
	}
	return dongle.deviceID, true
}

// pairedDeviceSnapshot returns the dongle's deviceID together with a copy of
// the paired-list entry at pairingID, read atomically under stateMu so the
// index can't shift out from under the caller between the two reads.
func pairedDeviceSnapshot(pairingID uint16) (deviceID uint16, target pairedDevice, ok bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	dongle, exists := deviceManager[selectedDongle]
	if !exists || dongle.pairingList == nil || int(pairingID) >= len(dongle.pairingList.pairedDevices) {
		return 0, pairedDevice{}, false
	}
	return dongle.deviceID, dongle.pairingList.pairedDevices[pairingID], true
}

func searchForNewDevices() error {
	if err := setDongleInBTPairing(true); err != nil {
		return err
	}

	if deviceID, exists := dongleDeviceID(); exists { // it can take up to 20 sec
		if err := returnCode(int(C.Jabra_SearchNewDevices(C.ushort(deviceID)))); err != nil {
			return err
		}
	}
	return nil
}

func setDongleInBTPairing(pairing bool) error {
	deviceID, exists := dongleDeviceID()
	if !exists {
		return fmt.Errorf("no dongle found")
	}

	if pairing {
		return returnCode(int(C.Jabra_SetBTPairing(C.ushort(deviceID))))
	}
	return returnCode(int(C.Jabra_StopBTPairing(C.ushort(deviceID))))
}

// newPairedDeviceC builds a single C.Jabra_PairedDevice struct for the given
// Go pairedDevice. Caller must free cDevice.deviceName.
func newPairedDeviceC(device pairedDevice) C.Jabra_PairedDevice {
	var cDevice C.Jabra_PairedDevice
	cDevice.deviceName = C.CString(device.deviceName)
	cDevice.isConnected = C.bool(device.isConnected)
	for j := 0; j < 6; j++ {
		cDevice.deviceBTAddr[j] = C.uint8_t(device.deviceBTAddr[j])
	}
	return cDevice
}

func connectNewDevice(pairingID uint16) error {
	deviceID, exists := dongleDeviceID()
	if !exists {
		return fmt.Errorf("no dongle found")
	}

	stateMu.Lock()
	if int(pairingID) >= len(searchDeviceList.pairedDevices) {
		stateMu.Unlock()
		return fmt.Errorf("invalid search selection")
	}
	target := searchDeviceList.pairedDevices[pairingID]
	stateMu.Unlock()

	cDevice := newPairedDeviceC(target)
	defer C.free(unsafe.Pointer(cDevice.deviceName))

	var returnErr error
	if err := returnCode(int(C.Jabra_ConnectNewDevice(C.ushort(deviceID), &cDevice))); err != nil {
		returnErr = err
	}
	if err := setDongleInBTPairing(false); err != nil {
		returnErr = err
	}

	return returnErr
}

func getSearchDeviceList(deviceID uint16) *pairingList {
	var searchDeviceList *pairingList

	cPairingList := C.Jabra_GetSearchDeviceList(C.ushort(deviceID))
	if cPairingList != nil {
		searchDeviceList = &pairingList{
			count:    uint16(cPairingList.count),
			listType: deviceListType(cPairingList.listType),
		}

		// safely cast a pointer to an array to a slice of a potentially unknown or large size
		// with a length and capacity limited to the actual number of items.
		// Similar to how in Go you might do: arr := [100]int{}; slice := arr[:10:10]
		// This creates a slice that references the first 10 elements with a capacity of 10
		cPairedDevices := (*[1 << 30]C.Jabra_PairedDevice)(unsafe.Pointer(cPairingList.pairedDevice))[:cPairingList.count:cPairingList.count]
		for _, cDevice := range cPairedDevices {
			bTDevice := pairedDevice{
				deviceName:  C.GoString(cDevice.deviceName),
				isConnected: bool(cDevice.isConnected),
			}
			copy(bTDevice.deviceBTAddr[:], C.GoBytes(unsafe.Pointer(&cDevice.deviceBTAddr[0]), 6))
			searchDeviceList.pairedDevices = append(searchDeviceList.pairedDevices, bTDevice)
		}

		C.Jabra_FreePairingList(cPairingList)
	}

	return searchDeviceList

}

func getPairingList(deviceID uint16) *pairingList {
	var createPairingList *pairingList

	cPairingList := C.Jabra_GetPairingList(C.ushort(deviceID))
	if cPairingList == nil {
		return &pairingList{
			count:         0,
			listType:      -1,
			pairedDevices: make([]pairedDevice, 0),
		}
	}
	createPairingList = &pairingList{
		count:    uint16(cPairingList.count),
		listType: deviceListType(cPairingList.listType),
	}

	cPairedDevices := (*[1 << 30]C.Jabra_PairedDevice)(unsafe.Pointer(cPairingList.pairedDevice))[:cPairingList.count:cPairingList.count]
	for _, cDevice := range cPairedDevices {
		bTDevice := pairedDevice{
			deviceName:  C.GoString(cDevice.deviceName),
			isConnected: bool(cDevice.isConnected),
		}
		copy(bTDevice.deviceBTAddr[:], C.GoBytes(unsafe.Pointer(&cDevice.deviceBTAddr[0]), 6))
		createPairingList.pairedDevices = append(createPairingList.pairedDevices, bTDevice)
	}
	C.Jabra_FreePairingList(cPairingList)

	return createPairingList
}

// Clear the pairingList
func clearPairingList() error {
	deviceID, exists := dongleDeviceID()
	if !exists {
		return fmt.Errorf("no dongle found")
	}
	return returnCode(int(C.Jabra_ClearPairingList(C.ushort(deviceID))))
}

// Remove device from pairingList where deviceListType is pairedDevices
func removeDeviceFromPairedlist(pairingID uint16) error {
	deviceID, target, ok := pairedDeviceSnapshot(pairingID)
	if !ok {
		return fmt.Errorf("invalid pairing selection")
	}

	cDevice := newPairedDeviceC(target)
	defer C.free(unsafe.Pointer(cDevice.deviceName))

	return returnCode(int(C.Jabra_ClearPairedDevice(C.ushort(deviceID), &cDevice)))
}

// Connect from pairingList where deviceListType is pairedDevices
func connectDeviceFromPairedlist(pairingID uint16) error {
	deviceID, target, ok := pairedDeviceSnapshot(pairingID)
	if !ok {
		return fmt.Errorf("invalid pairing selection")
	}

	cDevice := newPairedDeviceC(target)
	defer C.free(unsafe.Pointer(cDevice.deviceName))

	return returnCode(int(C.Jabra_ConnectPairedDevice(C.ushort(deviceID), &cDevice)))
}

// Disconnect from pairingList where deviceListType is pairedDevices
func disconnectDeviceFromPairedlist(pairingID uint16) error {
	deviceID, target, ok := pairedDeviceSnapshot(pairingID)
	if !ok {
		return fmt.Errorf("invalid pairing selection")
	}

	cDevice := newPairedDeviceC(target)
	defer C.free(unsafe.Pointer(cDevice.deviceName))

	return returnCode(int(C.Jabra_DisConnectPairedDevice(C.ushort(deviceID), &cDevice)))
}

func reconnectToDevice() error {
	deviceID, exists := dongleDeviceID()
	if !exists {
		return fmt.Errorf("no dongle found")
	}
	return returnCode(int(C.Jabra_ConnectBTDevice(C.ushort(deviceID))))
}

func disconnectBTDeviceFromDongle() error {
	deviceID, exists := dongleDeviceID()
	if !exists {
		return fmt.Errorf("no dongle found")
	}
	return returnCode(int(C.Jabra_DisconnectBTDevice(C.ushort(deviceID))))
}

func getAutoPairing() (bool, error) {
	deviceID, exists := dongleDeviceID()
	if !exists {
		return false, fmt.Errorf("no dongle")
	}
	return bool(C.Jabra_GetAutoPairing(C.ushort(deviceID))), nil
}

func setAutoPairing(autoPairing bool) error {
	deviceID, exists := dongleDeviceID()
	if !exists {
		return fmt.Errorf("no dongle")
	}
	return returnCode(int(C.Jabra_SetAutoPairing(C.ushort(deviceID), C.bool(autoPairing))))
}

/****************************************************************************/
/*                             BATTERY STATUS                               */
/****************************************************************************/

func getBatteryStatus(deviceID uint16) (*batteryStatus, error) {
	var cBatteryStatus *C.Jabra_BatteryStatus

	if err := returnCode(int(C.Jabra_GetBatteryStatusV2(C.ushort(deviceID), &cBatteryStatus))); err != nil {
		return nil, err
	}

	goBatteryStatus := &batteryStatus{
		levelInPercent:  uint8(cBatteryStatus.levelInPercent),
		charging:        bool(cBatteryStatus.charging),
		batteryLow:      bool(cBatteryStatus.batteryLow),
		component:       batteryComponent(cBatteryStatus.component),
		extraUnitsCount: uint32(cBatteryStatus.extraUnitsCount),
	}

	if cBatteryStatus.extraUnitsCount > 0 && cBatteryStatus.extraUnits != nil {
		extraUnits := (*[1 << 30]C.Jabra_BatteryStatusUnit)(unsafe.Pointer(cBatteryStatus.extraUnits))[:cBatteryStatus.extraUnitsCount:cBatteryStatus.extraUnitsCount]
		for _, unit := range extraUnits {
			goBatteryStatus.extraUnits = append(goBatteryStatus.extraUnits, batteryStatusUnit{
				levelInPercent: uint8(unit.levelInPercent),
				component:      batteryComponent(unit.component),
			})
		}
	}

	C.Jabra_FreeBatteryStatus(cBatteryStatus)

	return goBatteryStatus, nil
}
