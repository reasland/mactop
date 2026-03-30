package collector

/*
#include <IOKit/IOKitLib.h>
#include <IOKit/ps/IOPowerSources.h>
#include <IOKit/ps/IOPSKeys.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

// Helper to extract power source info from the CF dictionary.
struct power_info {
    int  has_battery;
    int  battery_percent;
    int  is_charging;
    int  max_capacity;
    int  current_capacity;
    int  time_remaining;
    char power_source[64];
};

static int get_power_source_info(struct power_info *out) {
    memset(out, 0, sizeof(*out));
    out->time_remaining = -1;

    CFTypeRef info = IOPSCopyPowerSourcesInfo();
    if (info == NULL) return -1;

    CFArrayRef list = IOPSCopyPowerSourcesList(info);
    if (list == NULL) {
        CFRelease(info);
        return -1;
    }

    CFIndex count = CFArrayGetCount(list);
    if (count == 0) {
        CFRelease(list);
        CFRelease(info);
        return 0;  // no battery
    }

    out->has_battery = 1;

    for (CFIndex i = 0; i < count; i++) {
        CFDictionaryRef ps = IOPSGetPowerSourceDescription(info, CFArrayGetValueAtIndex(list, i));
        if (ps == NULL) continue;

        CFNumberRef curCap = CFDictionaryGetValue(ps, CFSTR(kIOPSCurrentCapacityKey));
        CFNumberRef maxCap = CFDictionaryGetValue(ps, CFSTR(kIOPSMaxCapacityKey));
        CFBooleanRef charging = CFDictionaryGetValue(ps, CFSTR(kIOPSIsChargingKey));
        CFStringRef source = CFDictionaryGetValue(ps, CFSTR(kIOPSPowerSourceStateKey));
        CFNumberRef timeRem = CFDictionaryGetValue(ps, CFSTR(kIOPSTimeToEmptyKey));

        if (curCap) CFNumberGetValue(curCap, kCFNumberIntType, &out->current_capacity);
        if (maxCap) CFNumberGetValue(maxCap, kCFNumberIntType, &out->max_capacity);
        if (charging) out->is_charging = CFBooleanGetValue(charging) ? 1 : 0;
        if (source) CFStringGetCString(source, out->power_source, sizeof(out->power_source), kCFStringEncodingUTF8);
        if (timeRem) CFNumberGetValue(timeRem, kCFNumberIntType, &out->time_remaining);

        if (out->max_capacity > 0) {
            out->battery_percent = (out->current_capacity * 100) / out->max_capacity;
        }
    }

    CFRelease(list);
    CFRelease(info);
    return 0;
}
*/
import "C"

import (
	"math"

	"github.com/rileyeasland/mactop/internal/metrics"
	"github.com/rileyeasland/mactop/internal/platform"
)

// PowerCollector gathers battery and power source information.
type PowerCollector struct {
	Data metrics.PowerMetrics
}

func NewPowerCollector() *PowerCollector {
	return &PowerCollector{}
}

func (c *PowerCollector) Name() string { return "power" }

func (c *PowerCollector) Collect() error {
	var info C.struct_power_info
	rc := C.get_power_source_info(&info)

	if rc != 0 || info.has_battery == 0 {
		c.Data = metrics.PowerMetrics{
			HasBattery:  false,
			PowerSource: "AC Power",
		}
		return nil
	}

	c.Data = metrics.PowerMetrics{
		HasBattery:     true,
		BatteryPercent: int(info.battery_percent),
		IsCharging:     info.is_charging != 0,
		PowerSource:    C.GoString(&info.power_source[0]),
		TimeRemaining:  int(info.time_remaining),
	}

	// Try to get voltage and amperage from AppleSmartBattery for wattage calculation.
	c.readSmartBattery()

	return nil
}

func (c *PowerCollector) readSmartBattery() {
	svc, err := platform.IOKitGetMatchingService("AppleSmartBattery")
	if err != nil {
		return
	}
	defer svc.Release()

	props, err := svc.GetProperties()
	if err != nil {
		return
	}
	defer props.Release()

	if voltage, ok := props.GetInt64("Voltage"); ok {
		c.Data.Voltage = float64(voltage) / 1000.0 // mV to V
	}

	if amperage, ok := props.GetInt64("Amperage"); ok {
		c.Data.Amperage = float64(amperage) / 1000.0 // mA to A
	}

	if c.Data.Voltage > 0 {
		c.Data.Wattage = math.Abs(c.Data.Voltage * c.Data.Amperage)
	}
}
