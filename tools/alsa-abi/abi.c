/* Prints the sizes, offsets and ioctl numbers of the ALSA kernel interface (sound/asound.h) that
 * deck/internal/alsa uses. The test of the Go package compares its constants with this output. */
#include <stdio.h>
#include <stddef.h>
#include <sys/ioctl.h>
#include <sound/asound.h>

#define P(name, v) printf("%s %lu\n", name, (unsigned long)(v))
int main(void) {
	P("sizeof_hw_params", sizeof(struct snd_pcm_hw_params));
	P("off_hw_masks", offsetof(struct snd_pcm_hw_params, masks));
	P("off_hw_intervals", offsetof(struct snd_pcm_hw_params, intervals));
	P("off_hw_rmask", offsetof(struct snd_pcm_hw_params, rmask));
	P("off_hw_cmask", offsetof(struct snd_pcm_hw_params, cmask));
	P("off_hw_info", offsetof(struct snd_pcm_hw_params, info));
	P("off_hw_msbits", offsetof(struct snd_pcm_hw_params, msbits));
	P("off_hw_fifo_size", offsetof(struct snd_pcm_hw_params, fifo_size));
	P("sizeof_interval", sizeof(struct snd_interval));
	P("sizeof_sw_params", sizeof(struct snd_pcm_sw_params));
	P("off_sw_avail_min", offsetof(struct snd_pcm_sw_params, avail_min));
	P("off_sw_start_threshold", offsetof(struct snd_pcm_sw_params, start_threshold));
	P("off_sw_stop_threshold", offsetof(struct snd_pcm_sw_params, stop_threshold));
	P("off_sw_boundary", offsetof(struct snd_pcm_sw_params, boundary));
	P("off_sw_proto", offsetof(struct snd_pcm_sw_params, proto));
	P("sizeof_xferi", sizeof(struct snd_xferi));
	P("sizeof_ctl_elem_id", sizeof(struct snd_ctl_elem_id));
	P("sizeof_ctl_elem_value", sizeof(struct snd_ctl_elem_value));
	P("off_ctl_value", offsetof(struct snd_ctl_elem_value, value));
	P("sizeof_ctl_elem_info", sizeof(struct snd_ctl_elem_info));
	P("off_ctl_info_count", offsetof(struct snd_ctl_elem_info, count));
	P("off_ctl_info_value", offsetof(struct snd_ctl_elem_info, value));
	P("SNDRV_PCM_IOCTL_PVERSION", SNDRV_PCM_IOCTL_PVERSION);
	P("SNDRV_PCM_IOCTL_HW_REFINE", SNDRV_PCM_IOCTL_HW_REFINE);
	P("SNDRV_PCM_IOCTL_HW_PARAMS", SNDRV_PCM_IOCTL_HW_PARAMS);
	P("SNDRV_PCM_IOCTL_SW_PARAMS", SNDRV_PCM_IOCTL_SW_PARAMS);
	P("SNDRV_PCM_IOCTL_DELAY", SNDRV_PCM_IOCTL_DELAY);
	P("SNDRV_PCM_IOCTL_PREPARE", SNDRV_PCM_IOCTL_PREPARE);
	P("SNDRV_PCM_IOCTL_DROP", SNDRV_PCM_IOCTL_DROP);
	P("SNDRV_PCM_IOCTL_WRITEI_FRAMES", SNDRV_PCM_IOCTL_WRITEI_FRAMES);
	P("SNDRV_CTL_IOCTL_ELEM_INFO", SNDRV_CTL_IOCTL_ELEM_INFO);
	P("SNDRV_CTL_IOCTL_ELEM_READ", SNDRV_CTL_IOCTL_ELEM_READ);
	P("SNDRV_CTL_IOCTL_ELEM_WRITE", SNDRV_CTL_IOCTL_ELEM_WRITE);
	P("SNDRV_PCM_FORMAT_S16_LE", SNDRV_PCM_FORMAT_S16_LE);
	P("SNDRV_PCM_FORMAT_S32_LE", SNDRV_PCM_FORMAT_S32_LE);
	P("SNDRV_PCM_FORMAT_FLOAT_LE", SNDRV_PCM_FORMAT_FLOAT_LE);
	P("SNDRV_PCM_ACCESS_RW_INTERLEAVED", SNDRV_PCM_ACCESS_RW_INTERLEAVED);
	P("SNDRV_PCM_HW_PARAM_CHANNELS", SNDRV_PCM_HW_PARAM_CHANNELS);
	P("SNDRV_PCM_HW_PARAM_RATE", SNDRV_PCM_HW_PARAM_RATE);
	P("SNDRV_PCM_HW_PARAM_PERIOD_SIZE", SNDRV_PCM_HW_PARAM_PERIOD_SIZE);
	P("SNDRV_PCM_HW_PARAM_BUFFER_SIZE", SNDRV_PCM_HW_PARAM_BUFFER_SIZE);
	P("SNDRV_PCM_HW_PARAM_FIRST_INTERVAL", SNDRV_PCM_HW_PARAM_FIRST_INTERVAL);
	P("SNDRV_PCM_HW_PARAM_LAST_INTERVAL", SNDRV_PCM_HW_PARAM_LAST_INTERVAL);
	P("SNDRV_CTL_ELEM_IFACE_MIXER", SNDRV_CTL_ELEM_IFACE_MIXER);
	P("SNDRV_PCM_VERSION", SNDRV_PCM_VERSION);
	return 0;
}
