package alsa

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

// The structures and constants are those of the kernel header (tools/alsa-abi/abi-amd64.txt,
// printed by abi.c from sound/asound.h). arm64 has the same layout as amd64.
func TestABIMatchesKernelHeader(t *testing.T) {
	f, err := os.Open("../../../tools/alsa-abi/abi-amd64.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, v, _ := strings.Cut(sc.Text(), " ")
		n, _ := strconv.ParseUint(v, 10, 64)
		want[name] = n
	}
	var hw hwParams
	var sw swParams
	var v ctlElemValue
	var info ctlElemInfo
	got := map[string]uint64{
		"sizeof_hw_params":                  uint64(unsafe.Sizeof(hw)),
		"off_hw_masks":                      uint64(unsafe.Offsetof(hw.masks)),
		"off_hw_intervals":                  uint64(unsafe.Offsetof(hw.intervals)),
		"off_hw_rmask":                      uint64(unsafe.Offsetof(hw.rmask)),
		"off_hw_cmask":                      uint64(unsafe.Offsetof(hw.cmask)),
		"off_hw_info":                       uint64(unsafe.Offsetof(hw.info)),
		"off_hw_msbits":                     uint64(unsafe.Offsetof(hw.msbits)),
		"off_hw_fifo_size":                  uint64(unsafe.Offsetof(hw.fifoSize)),
		"sizeof_interval":                   uint64(unsafe.Sizeof(interval{})),
		"sizeof_sw_params":                  uint64(unsafe.Sizeof(sw)),
		"off_sw_avail_min":                  uint64(unsafe.Offsetof(sw.availMin)),
		"off_sw_start_threshold":            uint64(unsafe.Offsetof(sw.startThreshold)),
		"off_sw_stop_threshold":             uint64(unsafe.Offsetof(sw.stopThreshold)),
		"off_sw_boundary":                   uint64(unsafe.Offsetof(sw.boundary)),
		"off_sw_proto":                      uint64(unsafe.Offsetof(sw.proto)),
		"sizeof_xferi":                      uint64(unsafe.Sizeof(xferi{})),
		"sizeof_ctl_elem_id":                uint64(unsafe.Sizeof(ctlElemID{})),
		"sizeof_ctl_elem_value":             uint64(unsafe.Sizeof(v)),
		"off_ctl_value":                     uint64(unsafe.Offsetof(v.value)),
		"sizeof_ctl_elem_info":              uint64(unsafe.Sizeof(info)),
		"off_ctl_info_count":                uint64(unsafe.Offsetof(info.count)),
		"off_ctl_info_value":                uint64(unsafe.Offsetof(info.min)),
		"SNDRV_PCM_IOCTL_PVERSION":          ioctlPVersion,
		"SNDRV_PCM_IOCTL_HW_REFINE":         ioctlHWRefine,
		"SNDRV_PCM_IOCTL_HW_PARAMS":         ioctlHWParams,
		"SNDRV_PCM_IOCTL_SW_PARAMS":         ioctlSWParams,
		"SNDRV_PCM_IOCTL_DELAY":             ioctlDelay,
		"SNDRV_PCM_IOCTL_PREPARE":           ioctlPrepare,
		"SNDRV_PCM_IOCTL_DROP":              ioctlDrop,
		"SNDRV_PCM_IOCTL_WRITEI_FRAMES":     ioctlWriteIFrame,
		"SNDRV_CTL_IOCTL_ELEM_INFO":         ioctlElemInfo,
		"SNDRV_CTL_IOCTL_ELEM_READ":         ioctlElemRead,
		"SNDRV_CTL_IOCTL_ELEM_WRITE":        ioctlElemWrite,
		"SNDRV_PCM_FORMAT_S16_LE":           uint64(FormatS16LE),
		"SNDRV_PCM_FORMAT_S32_LE":           uint64(FormatS32LE),
		"SNDRV_PCM_FORMAT_FLOAT_LE":         uint64(FormatFloatLE),
		"SNDRV_PCM_ACCESS_RW_INTERLEAVED":   accessRWInterleaved,
		"SNDRV_PCM_HW_PARAM_CHANNELS":       paramChannels,
		"SNDRV_PCM_HW_PARAM_RATE":           paramRate,
		"SNDRV_PCM_HW_PARAM_PERIOD_SIZE":    paramPeriodSize,
		"SNDRV_PCM_HW_PARAM_BUFFER_SIZE":    paramBufferSize,
		"SNDRV_PCM_HW_PARAM_FIRST_INTERVAL": paramFirstInterval,
		"SNDRV_PCM_HW_PARAM_LAST_INTERVAL":  paramLastInterval,
		"SNDRV_CTL_ELEM_IFACE_MIXER":        ifaceMixer,
		"SNDRV_PCM_VERSION":                 pcmVersion,
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("%s is not checked", name)
		} else if g != w {
			t.Errorf("%s = %d, the kernel header has %d", name, g, w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d values checked, the header file has %d", len(got), len(want))
	}
}

func TestParseDevice(t *testing.T) {
	if c, d, err := ParseDevice("hw:1,3"); err != nil || c != 1 || d != 3 {
		t.Errorf("hw:1,3 = %d %d %v", c, d, err)
	}
	if _, _, err := ParseDevice("default"); err == nil {
		t.Error("default must be an error")
	}
}
