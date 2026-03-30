package platform

/*
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

// Private IOHIDEventSystem API declarations.
// These symbols exist in IOKit but have no public headers.
typedef void* IOHIDEventSystemClientRef;
typedef void* IOHIDServiceClientRef;
typedef void* IOHIDEventRef;

extern IOHIDEventSystemClientRef IOHIDEventSystemClientCreate(CFAllocatorRef);
extern void IOHIDEventSystemClientSetMatching(IOHIDEventSystemClientRef, CFDictionaryRef);
extern CFArrayRef IOHIDEventSystemClientCopyServices(IOHIDEventSystemClientRef);
extern CFTypeRef IOHIDServiceClientCopyProperty(IOHIDServiceClientRef, CFStringRef);
extern IOHIDEventRef IOHIDServiceClientCopyEvent(IOHIDServiceClientRef, int64_t, int64_t, int64_t);
extern double IOHIDEventGetFloatValue(IOHIDEventRef, int64_t);

// Constants for temperature sensor matching and event reading.
#define kIOHIDPageAppleVendor                   0xFF00
#define kIOHIDUsageAppleVendorTemperatureSensor 0x05
#define kIOHIDEventTypeTemperature              0x0F
#define kIOHIDEventFieldTemperatureLevel        (kIOHIDEventTypeTemperature << 16)

// hidCreateClient creates an IOHIDEventSystemClient.
// Returns NULL if the API is unavailable.
static IOHIDEventSystemClientRef hidCreateClient(void) {
    return IOHIDEventSystemClientCreate(kCFAllocatorDefault);
}

// hidSetTempMatching sets the matching filter for temperature sensors.
static void hidSetTempMatching(IOHIDEventSystemClientRef client) {
    CFStringRef keys[2];
    CFNumberRef vals[2];

    int page = kIOHIDPageAppleVendor;
    int usage = kIOHIDUsageAppleVendorTemperatureSensor;

    keys[0] = CFSTR("PrimaryUsagePage");
    keys[1] = CFSTR("PrimaryUsage");
    vals[0] = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &page);
    vals[1] = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &usage);

    CFDictionaryRef matching = CFDictionaryCreate(kCFAllocatorDefault,
        (const void**)keys, (const void**)vals, 2,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);

    IOHIDEventSystemClientSetMatching(client, matching);

    CFRelease(vals[0]);
    CFRelease(vals[1]);
    CFRelease(matching);
}

// hidCopyServices returns the array of matching services.
// Caller must CFRelease the returned array.
static CFArrayRef hidCopyServices(IOHIDEventSystemClientRef client) {
    return IOHIDEventSystemClientCopyServices(client);
}

// hidServiceCount returns the number of services in the array.
static CFIndex hidServiceCount(CFArrayRef services) {
    if (services == NULL) return 0;
    return CFArrayGetCount(services);
}

// hidServiceAtIndex returns the service at the given index.
static IOHIDServiceClientRef hidServiceAtIndex(CFArrayRef services, CFIndex idx) {
    return (IOHIDServiceClientRef)CFArrayGetValueAtIndex(services, idx);
}

// hidGetProductName returns the "Product" property of a service as a C string.
// Caller must free() the result. Returns NULL if unavailable.
static char* hidGetProductName(IOHIDServiceClientRef service) {
    CFTypeRef prop = IOHIDServiceClientCopyProperty(service, CFSTR("Product"));
    if (prop == NULL) return NULL;
    if (CFGetTypeID(prop) != CFStringGetTypeID()) {
        CFRelease(prop);
        return NULL;
    }
    CFStringRef str = (CFStringRef)prop;
    CFIndex len = CFStringGetLength(str);
    CFIndex maxSize = CFStringGetMaximumSizeForEncoding(len, kCFStringEncodingUTF8) + 1;
    char *buf = (char*)malloc(maxSize);
    if (!CFStringGetCString(str, buf, maxSize, kCFStringEncodingUTF8)) {
        free(buf);
        CFRelease(prop);
        return NULL;
    }
    CFRelease(prop);
    return buf;
}

// hidReadTemperature reads the temperature event from a service.
// Returns the temperature in Celsius, or -999 on failure.
static double hidReadTemperature(IOHIDServiceClientRef service) {
    IOHIDEventRef event = IOHIDServiceClientCopyEvent(service,
        kIOHIDEventTypeTemperature, 0, 0);
    if (event == NULL) return -999.0;
    double val = IOHIDEventGetFloatValue(event, kIOHIDEventFieldTemperatureLevel);
    CFRelease(event);
    return val;
}

// hidReleaseClient releases the client. CFRelease works on the opaque pointer.
static void hidReleaseClient(IOHIDEventSystemClientRef client) {
    if (client != NULL) CFRelease(client);
}

// hidReleaseArray releases a CFArrayRef.
static void hidReleaseArray(CFArrayRef arr) {
    if (arr != NULL) CFRelease(arr);
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// HIDTempSensor represents a temperature sensor discovered via IOHIDEventSystem.
type HIDTempSensor struct {
	Name  string
	Value float64 // degrees Celsius
}

// HIDThermalReader reads temperature sensors via the IOHIDEventSystem API.
// This works on macOS 26+ where direct SMC access is blocked.
type HIDThermalReader struct {
	client      C.IOHIDEventSystemClientRef
	cachedNames []string // sensor names cached from first read
}

// NewHIDThermalReader creates a new reader. Returns error if the API is unavailable.
func NewHIDThermalReader() (*HIDThermalReader, error) {
	client := C.hidCreateClient()
	if client == nil {
		return nil, errHIDUnavailable
	}
	C.hidSetTempMatching(client)
	return &HIDThermalReader{client: client}, nil
}

var errHIDUnavailable = errors.New("IOHIDEventSystem unavailable")

// ReadTemperatures reads all available temperature sensors.
func (r *HIDThermalReader) ReadTemperatures() ([]HIDTempSensor, error) {
	services := C.hidCopyServices(r.client)
	defer C.hidReleaseArray(services)

	count := int(C.hidServiceCount(services))
	if count == 0 {
		return nil, nil
	}

	useCache := len(r.cachedNames) > 0 && count == len(r.cachedNames)

	seen := make(map[string]struct{}, count)
	sensors := make([]HIDTempSensor, 0, count)
	var names []string
	if !useCache {
		names = make([]string, 0, count)
	}

	for i := 0; i < count; i++ {
		svc := C.hidServiceAtIndex(services, C.CFIndex(i))

		var name string
		if useCache {
			name = r.cachedNames[i]
		} else {
			cName := C.hidGetProductName(svc)
			if cName == nil {
				names = append(names, "")
				continue
			}
			name = C.GoString(cName)
			C.free(unsafe.Pointer(cName))
			names = append(names, name)
		}

		if name == "" {
			continue
		}

		// Deduplicate by name; keep first occurrence.
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}

		temp := float64(C.hidReadTemperature(svc))
		if temp < 0 || temp > 150 {
			continue
		}

		sensors = append(sensors, HIDTempSensor{
			Name:  name,
			Value: temp,
		})
	}

	if !useCache && len(names) == count {
		r.cachedNames = names
	}

	return sensors, nil
}

// Close releases the HID event system client.
func (r *HIDThermalReader) Close() {
	if r.client != nil {
		C.hidReleaseClient(r.client)
		r.client = nil
	}
}
