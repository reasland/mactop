package platform

/*
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

// Helper to get a CFString from a Go string for use in CF dictionaries.
static CFStringRef cfStr(const char *s) {
    return CFStringCreateWithCString(kCFAllocatorDefault, s, kCFStringEncodingUTF8);
}

// Helper to extract an int64 from a CFNumber.
static int cfNumberToInt64(CFTypeRef ref, int64_t *out) {
    if (ref == NULL || CFGetTypeID(ref) != CFNumberGetTypeID()) return 0;
    return CFNumberGetValue((CFNumberRef)ref, kCFNumberSInt64Type, out);
}

// Helper to extract a boolean from a CFBoolean.
static int cfBooleanValue(CFTypeRef ref) {
    if (ref == NULL || CFGetTypeID(ref) != CFBooleanGetTypeID()) return -1;
    return CFBooleanGetValue((CFBooleanRef)ref) ? 1 : 0;
}

// Helper to extract a C string from a CFString. Caller must free().
static char* cfStringToCString(CFTypeRef ref) {
    if (ref == NULL || CFGetTypeID(ref) != CFStringGetTypeID()) return NULL;
    CFIndex len = CFStringGetLength((CFStringRef)ref);
    CFIndex maxSize = CFStringGetMaximumSizeForEncoding(len, kCFStringEncodingUTF8) + 1;
    char *buf = (char*)malloc(maxSize);
    if (!CFStringGetCString((CFStringRef)ref, buf, maxSize, kCFStringEncodingUTF8)) {
        free(buf);
        return NULL;
    }
    return buf;
}

// Wrappers that handle CFMutableDictionaryRef -> CFDictionaryRef casts at the C level,
// because CGo treats them as distinct types.

static io_service_t iokit_get_matching_service(const char *className) {
    CFMutableDictionaryRef matching = IOServiceMatching(className);
    return IOServiceGetMatchingService(kIOMainPortDefault, matching);
}

static kern_return_t iokit_get_matching_services(const char *className, io_iterator_t *iterator) {
    CFMutableDictionaryRef matching = IOServiceMatching(className);
    return IOServiceGetMatchingServices(kIOMainPortDefault, matching, iterator);
}

static kern_return_t iokit_get_properties(io_registry_entry_t entry, CFMutableDictionaryRef *props) {
    return IORegistryEntryCreateCFProperties(entry, props, kCFAllocatorDefault, 0);
}

// Dict lookup helpers that work with CFMutableDictionaryRef.
static const void* dict_get_value(CFMutableDictionaryRef dict, const char *key) {
    CFStringRef cfKey = cfStr(key);
    if (cfKey == NULL) return NULL;
    const void *val = CFDictionaryGetValue((CFDictionaryRef)dict, cfKey);
    CFRelease(cfKey);
    return val;
}

static int dict_get_int64(CFMutableDictionaryRef dict, const char *key, int64_t *out) {
    const void *val = dict_get_value(dict, key);
    if (val == NULL) return 0;
    return cfNumberToInt64((CFTypeRef)val, out);
}

static int dict_is_cf_dict(const void *val) {
    if (val == NULL) return 0;
    return CFGetTypeID((CFTypeRef)val) == CFDictionaryGetTypeID() ? 1 : 0;
}

// Get a sub-dictionary from a mutable dict. Returns NULL if not found or not a dict.
static CFMutableDictionaryRef dict_get_dict(CFMutableDictionaryRef dict, const char *key) {
    const void *val = dict_get_value(dict, key);
    if (val == NULL) return NULL;
    if (CFGetTypeID((CFTypeRef)val) != CFDictionaryGetTypeID()) return NULL;
    // The sub-dictionary is actually immutable inside the parent, but we cast
    // to mutable to keep the Go side using one type consistently.
    return (CFMutableDictionaryRef)val;
}

static int dict_get_bool(CFMutableDictionaryRef dict, const char *key, int *out) {
    const void *val = dict_get_value(dict, key);
    if (val == NULL) return 0;
    int r = cfBooleanValue((CFTypeRef)val);
    if (r < 0) return 0;
    *out = r;
    return 1;
}

static char* dict_get_string(CFMutableDictionaryRef dict, const char *key) {
    const void *val = dict_get_value(dict, key);
    if (val == NULL) return NULL;
    return cfStringToCString((CFTypeRef)val);
}

// Release a CFMutableDictionaryRef.
static void cf_release_dict(CFMutableDictionaryRef dict) {
    if (dict != NULL) CFRelease(dict);
}

// Check if a CFMutableDictionaryRef is NULL.
static int cf_dict_is_null(CFMutableDictionaryRef dict) {
    return dict == NULL ? 1 : 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// IOKitService represents an open IOKit service for property reading.
type IOKitService struct {
	entry C.io_registry_entry_t
}

// IOKitGetMatchingService finds the first service matching the given class name.
func IOKitGetMatchingService(className string) (*IOKitService, error) {
	cName := C.CString(className)
	defer C.free(unsafe.Pointer(cName))

	entry := C.iokit_get_matching_service(cName)
	if entry == 0 {
		return nil, fmt.Errorf("IOKit service %q not found", className)
	}
	return &IOKitService{entry: entry}, nil
}

// Release releases the IOKit object.
func (s *IOKitService) Release() {
	C.IOObjectRelease(s.entry)
}

// Entry returns the raw io_registry_entry_t.
func (s *IOKitService) Entry() C.io_registry_entry_t {
	return s.entry
}

// CFDict wraps a CFMutableDictionaryRef for safe use in Go.
type CFDict struct {
	ref C.CFMutableDictionaryRef
}

// Release releases the CF dictionary.
func (d *CFDict) Release() {
	C.cf_release_dict(d.ref)
}

// GetProperties reads all properties from the registry entry.
func (s *IOKitService) GetProperties() (*CFDict, error) {
	var props C.CFMutableDictionaryRef
	kr := C.iokit_get_properties(s.entry, &props)
	if kr != C.kIOReturnSuccess {
		return nil, fmt.Errorf("IORegistryEntryCreateCFProperties failed: 0x%x", kr)
	}
	return &CFDict{ref: props}, nil
}

// GetInt64 extracts an int64 value by string key.
func (d *CFDict) GetInt64(key string) (int64, bool) {
	cKey := C.CString(key)
	defer C.free(unsafe.Pointer(cKey))

	var out C.int64_t
	if C.dict_get_int64(d.ref, cKey, &out) == 0 {
		return 0, false
	}
	return int64(out), true
}

// GetDict extracts a nested dictionary by string key.
// The returned CFDict does NOT own the reference (do not Release it).
func (d *CFDict) GetDict(key string) (*CFDict, bool) {
	cKey := C.CString(key)
	defer C.free(unsafe.Pointer(cKey))

	sub := C.dict_get_dict(d.ref, cKey)
	if C.cf_dict_is_null(sub) != 0 {
		return nil, false
	}
	return &CFDict{ref: sub}, true
}

// GetBool extracts a boolean by string key.
func (d *CFDict) GetBool(key string) (bool, bool) {
	cKey := C.CString(key)
	defer C.free(unsafe.Pointer(cKey))

	var out C.int
	if C.dict_get_bool(d.ref, cKey, &out) == 0 {
		return false, false
	}
	return out == 1, true
}

// GetString extracts a string by string key.
func (d *CFDict) GetString(key string) (string, bool) {
	cKey := C.CString(key)
	defer C.free(unsafe.Pointer(cKey))

	cStr := C.dict_get_string(d.ref, cKey)
	if cStr == nil {
		return "", false
	}
	defer C.free(unsafe.Pointer(cStr))
	return C.GoString(cStr), true
}

// IOKitIterateMatching iterates over all services matching the given class name.
// For each matching service, it reads the properties and calls fn with the CFDict.
// The CFDict is released after fn returns.
func IOKitIterateMatching(className string, fn func(props *CFDict) error) error {
	cName := C.CString(className)
	defer C.free(unsafe.Pointer(cName))

	var iterator C.io_iterator_t
	kr := C.iokit_get_matching_services(cName, &iterator)
	if kr != C.kIOReturnSuccess {
		return fmt.Errorf("IOServiceGetMatchingServices(%s) failed: 0x%x", className, kr)
	}
	defer C.IOObjectRelease(C.io_object_t(iterator))

	for {
		entry := C.IOIteratorNext(iterator)
		if entry == 0 {
			break
		}

		var props C.CFMutableDictionaryRef
		pkr := C.iokit_get_properties(entry, &props)
		C.IOObjectRelease(entry)
		if pkr != C.kIOReturnSuccess {
			continue
		}

		dict := &CFDict{ref: props}
		err := fn(dict)
		dict.Release()
		if err != nil {
			return err
		}
	}
	return nil
}
