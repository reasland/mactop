package collector

/*
#include <sys/sysctl.h>
#include <sys/socket.h>
#include <net/if.h>
#include <net/route.h>
#include <net/if_dl.h>
#include <stdlib.h>
#include <string.h>

// if_msghdr2 is the extended interface message header returned by NET_RT_IFLIST2.
// We need the ifi_ibytes and ifi_obytes fields from the embedded if_data64.
// On macOS, if_msghdr2 contains if_data64 with 64-bit counters.

struct net_iface_stats {
    unsigned int  ifm_index;
    uint64_t      ifi_ibytes;
    uint64_t      ifi_obytes;
    uint64_t      ifi_ipackets;
    uint64_t      ifi_opackets;
};

// Parse the NET_RT_IFLIST2 buffer and extract per-interface stats.
// Returns the number of interfaces found.
//
// The buffer contains mixed routing message types (RTM_IFINFO2, RTM_NEWADDR, etc.).
// Non-IFINFO messages use smaller structs, so we read the common 4-byte header
// (msglen + version + type) first and only cast to if_msghdr2 for RTM_IFINFO2.
static int parse_iflist2(const char *buf, size_t bufLen,
                          struct net_iface_stats *out, int maxOut) {
    int count = 0;
    const char *end = buf + bufLen;
    const char *ptr = buf;

    while (ptr + 4 <= end && count < maxOut) {
        // All routing messages start with {u_short msglen, u_char version, u_char type}
        unsigned short msglen = *(const unsigned short *)ptr;
        unsigned char msgtype = *(const unsigned char *)(ptr + 3);

        if (msglen == 0) break;
        if (ptr + msglen > end) break;

        if (msgtype == RTM_IFINFO2 && msglen >= sizeof(struct if_msghdr2)) {
            struct if_msghdr2 *ifm2 = (struct if_msghdr2 *)ptr;
            out[count].ifm_index = ifm2->ifm_index;
            out[count].ifi_ibytes = ifm2->ifm_data.ifi_ibytes;
            out[count].ifi_obytes = ifm2->ifm_data.ifi_obytes;
            out[count].ifi_ipackets = ifm2->ifm_data.ifi_ipackets;
            out[count].ifi_opackets = ifm2->ifm_data.ifi_opackets;
            count++;
        }

        ptr += msglen;
    }

    return count;
}

// Get interface name from index.
static int get_if_name(unsigned int idx, char *name, size_t nameLen) {
    if (if_indextoname(idx, name) == NULL) return -1;
    return 0;
}
*/
import "C"

import (
	"time"
	"unsafe"

	"github.com/rileyeasland/mactop/internal/metrics"
)

const maxInterfaces = 64

// netIfPrev stores the previous sample for delta computation.
type netIfPrev struct {
	bytesIn  uint64
	bytesOut uint64
}

// NetworkCollector gathers per-interface network throughput.
type NetworkCollector struct {
	Data     []metrics.NetworkInterface
	prev     map[string]netIfPrev
	prevTime time.Time
}

func NewNetworkCollector() *NetworkCollector {
	return &NetworkCollector{
		prev: make(map[string]netIfPrev),
	}
}

func (c *NetworkCollector) Name() string { return "network" }

func (c *NetworkCollector) Collect() error {
	now := time.Now()

	// Query required buffer size.
	mib := [6]C.int{C.CTL_NET, C.PF_ROUTE, 0, 0, C.NET_RT_IFLIST2, 0}
	var bufLen C.size_t
	rc := C.sysctl(&mib[0], 6, nil, &bufLen, nil, 0)
	if rc != 0 {
		return nil // non-fatal
	}

	buf := make([]byte, bufLen)
	rc = C.sysctl(&mib[0], 6, unsafe.Pointer(&buf[0]), &bufLen, nil, 0)
	if rc != 0 {
		return nil
	}

	var stats [maxInterfaces]C.struct_net_iface_stats
	count := C.parse_iflist2((*C.char)(unsafe.Pointer(&buf[0])), C.size_t(bufLen),
		&stats[0], C.int(maxInterfaces))

	elapsed := now.Sub(c.prevTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	var result []metrics.NetworkInterface

	for i := 0; i < int(count); i++ {
		var nameBuf [C.IFNAMSIZ]C.char
		if C.get_if_name(stats[i].ifm_index, &nameBuf[0], C.IFNAMSIZ) != 0 {
			continue
		}
		name := C.GoString(&nameBuf[0])

		// Skip loopback.
		if name == "lo0" {
			continue
		}

		bytesIn := uint64(stats[i].ifi_ibytes)
		bytesOut := uint64(stats[i].ifi_obytes)

		iface := metrics.NetworkInterface{
			Name:     name,
			BytesIn:  bytesIn,
			BytesOut: bytesOut,
		}

		// Compute per-second rates from delta.
		if prev, ok := c.prev[name]; ok && !c.prevTime.IsZero() {
			// Check for counter wraparound before subtracting.
			var dIn uint64
			if bytesIn >= prev.bytesIn {
				dIn = bytesIn - prev.bytesIn
			}
			var dOut uint64
			if bytesOut >= prev.bytesOut {
				dOut = bytesOut - prev.bytesOut
			}

			iface.BytesInPS = float64(dIn) / elapsed
			iface.BytesOutPS = float64(dOut) / elapsed
		}

		result = append(result, iface)
	}

	// Store current state for next delta.
	newPrev := make(map[string]netIfPrev, len(result))
	for _, iface := range result {
		newPrev[iface.Name] = netIfPrev{
			bytesIn:  iface.BytesIn,
			bytesOut: iface.BytesOut,
		}
	}
	c.prev = newPrev
	c.prevTime = now

	// Sort by total traffic and limit to top 5.
	if len(result) > 5 {
		// Simple selection: keep the top 5 by total bytes.
		for i := 0; i < 5 && i < len(result); i++ {
			maxIdx := i
			maxBytes := result[i].BytesIn + result[i].BytesOut
			for j := i + 1; j < len(result); j++ {
				jBytes := result[j].BytesIn + result[j].BytesOut
				if jBytes > maxBytes {
					maxIdx = j
					maxBytes = jBytes
				}
			}
			result[i], result[maxIdx] = result[maxIdx], result[i]
		}
		result = result[:5]
	}

	c.Data = result
	return nil
}
