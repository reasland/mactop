package smc

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <mach/mach.h>
#include <stdlib.h>
#include <string.h>

static mach_port_t get_mach_task_self(void) {
    return mach_task_self();
}

#define SMC_TYPE_FLT  0x666c7420  // "flt "
#define SMC_TYPE_SP78 0x73703738  // "sp78"

enum {
    kSMCUserClientOpen  = 0,
    kSMCUserClientClose = 1,
    kSMCHandleYPCEvent  = 2,
    kSMCReadKey         = 5,
    kSMCWriteKey        = 6,
    kSMCGetKeyCount     = 7,
    kSMCGetKeyFromIndex = 8,
    kSMCGetKeyInfo      = 9,
};

// These sub-structs must match the kernel driver's layout exactly.
// Using sub-structs ensures the compiler inserts the same alignment
// padding as the kernel expects. Total size: 80 bytes.

typedef struct {
    char                  major;
    char                  minor;
    char                  build;
    char                  reserved[1];
    unsigned short        release;
} SMCKeyData_vers_t;          // 6 bytes, 2-byte alignment

typedef struct {
    unsigned short        version;
    unsigned short        length;
    unsigned int          cpuPLimit;
    unsigned int          gpuPLimit;
    unsigned int          memPLimit;
} SMCKeyData_pLimitData_t;    // 16 bytes, 4-byte alignment

typedef struct {
    unsigned int          dataSize;
    unsigned int          dataType;
    char                  dataAttributes;
} SMCKeyData_keyInfo_t;       // 9 bytes -> 12 with trailing padding, 4-byte alignment

typedef struct {
    unsigned int                key;
    SMCKeyData_vers_t           vers;
    SMCKeyData_pLimitData_t     pLimitData;
    SMCKeyData_keyInfo_t        keyInfo;
    char                        result;
    unsigned char               status;
    unsigned char               data8;
    unsigned int                data32;
    unsigned char               bytes[32];
} SMCKeyData_t;               // 80 bytes

_Static_assert(sizeof(SMCKeyData_t) == 80, "SMCKeyData_t must be 80 bytes to match kernel driver");

static io_service_t smc_get_matching_service(const char *name) {
    CFMutableDictionaryRef matching = IOServiceMatching(name);
    return IOServiceGetMatchingService(kIOMainPortDefault, matching);
}

static kern_return_t smcGetKeyInfo(io_connect_t conn, uint32_t key,
                                    uint32_t *dataType, uint32_t *dataSize) {
    SMCKeyData_t input = {};
    SMCKeyData_t output = {};
    size_t outputSize = sizeof(output);

    input.key = key;

    kern_return_t kr = IOConnectCallStructMethod(conn, kSMCGetKeyInfo,
        &input, sizeof(input), &output, &outputSize);
    if (kr != kIOReturnSuccess) return kr;

    *dataType = output.keyInfo.dataType;
    *dataSize = output.keyInfo.dataSize;
    return kIOReturnSuccess;
}

static kern_return_t smcReadKey(io_connect_t conn, uint32_t key,
                                 uint32_t dataType, uint32_t dataSize,
                                 uint8_t *outBytes) {
    SMCKeyData_t input = {};
    SMCKeyData_t output = {};
    size_t outputSize = sizeof(output);

    input.key = key;
    input.keyInfo.dataType = dataType;
    input.keyInfo.dataSize = dataSize;

    kern_return_t kr = IOConnectCallStructMethod(conn, kSMCReadKey,
        &input, sizeof(input), &output, &outputSize);
    if (kr != kIOReturnSuccess) return kr;

    uint32_t copySize = dataSize;
    if (copySize > sizeof(output.bytes)) copySize = sizeof(output.bytes);
    memcpy(outBytes, output.bytes, copySize);
    return kIOReturnSuccess;
}

static kern_return_t smcGetKeyCount(io_connect_t conn, uint32_t *count) {
    SMCKeyData_t input = {};
    SMCKeyData_t output = {};
    size_t outputSize = sizeof(output);

    kern_return_t kr = IOConnectCallStructMethod(conn, kSMCGetKeyCount,
        &input, sizeof(input), &output, &outputSize);
    if (kr != kIOReturnSuccess) return kr;

    *count = output.data32;
    return kIOReturnSuccess;
}

static kern_return_t smcGetKeyAtIndex(io_connect_t conn, uint32_t index, uint32_t *key) {
    SMCKeyData_t input = {};
    SMCKeyData_t output = {};
    size_t outputSize = sizeof(output);

    input.data32 = index;

    kern_return_t kr = IOConnectCallStructMethod(conn, kSMCGetKeyFromIndex,
        &input, sizeof(input), &output, &outputSize);
    if (kr != kIOReturnSuccess) return kr;

    *key = output.key;
    return kIOReturnSuccess;
}
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unsafe"
)

// keyInfoCache stores the data type and size for an SMC key,
// which never change for a given key during the lifetime of a connection.
type keyInfoCache struct {
	dataType uint32
	dataSize uint32
}

// Connection holds an open handle to the AppleSMC IOKit service.
type Connection struct {
	conn     C.io_connect_t
	keyCache map[uint32]keyInfoCache
}

// encodeKey converts a 4-character SMC key to its uint32 big-endian
// representation in pure Go, avoiding a CGo call to smcKeyEncode.
func encodeKey(key string) uint32 {
	return uint32(key[0])<<24 | uint32(key[1])<<16 | uint32(key[2])<<8 | uint32(key[3])
}

// Open establishes a connection to AppleSMC.
func Open() (*Connection, error) {
	cName := C.CString("AppleSMC")
	defer C.free(unsafe.Pointer(cName))

	service := C.smc_get_matching_service(cName)
	if service == 0 {
		return nil, errors.New("AppleSMC service not found")
	}
	defer C.IOObjectRelease(C.io_object_t(service))

	var conn C.io_connect_t
	kr := C.IOServiceOpen(service, C.get_mach_task_self(), 0, &conn)
	if kr != C.kIOReturnSuccess {
		return nil, fmt.Errorf("IOServiceOpen(AppleSMC) failed: 0x%x", kr)
	}
	return &Connection{conn: conn, keyCache: make(map[uint32]keyInfoCache)}, nil
}

// Close releases the SMC connection.
func (c *Connection) Close() error {
	if c.conn != 0 {
		C.IOServiceClose(c.conn)
		c.conn = 0
	}
	return nil
}

// ReadFloat reads a temperature value for the given 4-char SMC key.
// It handles "flt " (float32) and "sp78" (signed 7.8 fixed-point) data types.
func (c *Connection) ReadFloat(key string) (float64, error) {
	if len(key) != 4 {
		return 0, fmt.Errorf("SMC key must be exactly 4 characters: %q", key)
	}

	encodedKey := encodeKey(key)

	// Step 1: Get key info (type and size), using cache when available.
	var dataType uint32
	var dataSize uint32
	if cached, ok := c.keyCache[encodedKey]; ok {
		dataType = cached.dataType
		dataSize = cached.dataSize
	} else {
		var cDataType C.uint32_t
		var cDataSize C.uint32_t
		kr := C.smcGetKeyInfo(c.conn, C.uint32_t(encodedKey), &cDataType, &cDataSize)
		if kr != C.kIOReturnSuccess {
			return 0, fmt.Errorf("SMC GetKeyInfo(%s) failed: 0x%x", key, kr)
		}
		dataType = uint32(cDataType)
		dataSize = uint32(cDataSize)
		c.keyCache[encodedKey] = keyInfoCache{dataType: dataType, dataSize: dataSize}
	}

	// Step 2: Read the key value.
	var rawBytes [32]C.uint8_t
	kr := C.smcReadKey(c.conn, C.uint32_t(encodedKey), C.uint32_t(dataType), C.uint32_t(dataSize), &rawBytes[0])
	if kr != C.kIOReturnSuccess {
		return 0, fmt.Errorf("SMC ReadKey(%s) failed: 0x%x", key, kr)
	}

	// Step 3: Parse according to data type.
	goBytes := make([]byte, int(dataSize))
	for i := 0; i < int(dataSize); i++ {
		goBytes[i] = byte(rawBytes[i])
	}

	switch dataType {
	case 0x666c7420: // "flt " - IEEE 754 float32, little-endian
		if len(goBytes) < 4 {
			return 0, fmt.Errorf("SMC key %s: flt data too short (%d bytes)", key, len(goBytes))
		}
		bits := binary.LittleEndian.Uint32(goBytes[:4])
		return float64(math.Float32frombits(bits)), nil

	case 0x73703738: // "sp78" - signed 7.8 fixed-point, big-endian
		if len(goBytes) < 2 {
			return 0, fmt.Errorf("SMC key %s: sp78 data too short (%d bytes)", key, len(goBytes))
		}
		raw := int16(binary.BigEndian.Uint16(goBytes[:2]))
		return float64(raw) / 256.0, nil

	default:
		// Try treating as float32 anyway.
		if len(goBytes) >= 4 {
			bits := binary.LittleEndian.Uint32(goBytes[:4])
			return float64(math.Float32frombits(bits)), nil
		}
		return 0, fmt.Errorf("SMC key %s: unknown data type 0x%x", key, dataType)
	}
}

// DiscoverTempSensors enumerates all SMC keys starting with 'T'
// that return float/sp78 temperature-like values (0-150 C).
// Returns an error if key enumeration is not available.
func (c *Connection) DiscoverTempSensors() ([]SensorDef, error) {
	var count C.uint32_t
	kr := C.smcGetKeyCount(c.conn, &count)
	if kr != C.kIOReturnSuccess || count == 0 {
		return nil, fmt.Errorf("key enumeration not available: 0x%x", kr)
	}

	var sensors []SensorDef
	for i := uint32(0); i < uint32(count); i++ {
		var key C.uint32_t
		kr := C.smcGetKeyAtIndex(c.conn, C.uint32_t(i), &key)
		if kr != C.kIOReturnSuccess {
			continue
		}

		// Check if key starts with 'T'.
		ch0 := byte((uint32(key) >> 24) & 0xFF)
		if ch0 != 'T' {
			continue
		}

		keyStr := fmt.Sprintf("%c%c%c%c",
			(uint32(key)>>24)&0xFF, (uint32(key)>>16)&0xFF,
			(uint32(key)>>8)&0xFF, uint32(key)&0xFF)

		// Try reading the value; skip keys that fail or are out of range.
		val, err := c.ReadFloat(keyStr)
		if err != nil {
			continue
		}
		if val < 0 || val > 150 {
			continue
		}

		sensors = append(sensors, SensorDef{Key: keyStr, Name: keyStr})
	}
	return sensors, nil
}

// ReadKey reads the raw bytes for the given 4-char SMC key.
func (c *Connection) ReadKey(key string) ([]byte, uint32, error) {
	if len(key) != 4 {
		return nil, 0, fmt.Errorf("SMC key must be exactly 4 characters: %q", key)
	}

	encodedKey := encodeKey(key)

	// Get key info, using cache when available.
	var dataType uint32
	var dataSize uint32
	if cached, ok := c.keyCache[encodedKey]; ok {
		dataType = cached.dataType
		dataSize = cached.dataSize
	} else {
		var cDataType C.uint32_t
		var cDataSize C.uint32_t
		kr := C.smcGetKeyInfo(c.conn, C.uint32_t(encodedKey), &cDataType, &cDataSize)
		if kr != C.kIOReturnSuccess {
			return nil, 0, fmt.Errorf("SMC GetKeyInfo(%s) failed: 0x%x", key, kr)
		}
		dataType = uint32(cDataType)
		dataSize = uint32(cDataSize)
		c.keyCache[encodedKey] = keyInfoCache{dataType: dataType, dataSize: dataSize}
	}

	var rawBytes [32]C.uint8_t
	kr := C.smcReadKey(c.conn, C.uint32_t(encodedKey), C.uint32_t(dataType), C.uint32_t(dataSize), &rawBytes[0])
	if kr != C.kIOReturnSuccess {
		return nil, 0, fmt.Errorf("SMC ReadKey(%s) failed: 0x%x", key, kr)
	}

	goBytes := make([]byte, int(dataSize))
	for i := 0; i < int(dataSize); i++ {
		goBytes[i] = byte(rawBytes[i])
	}

	return goBytes, dataType, nil
}
