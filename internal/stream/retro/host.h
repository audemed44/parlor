// A headless libretro frontend: one core, one game, in this process.
// Cores that render with OpenGL draw into a framebuffer on a GBM render
// node through EGL (no display server); every frame is read back for the
// encoder.
#ifndef PARLOR_HOST_H
#define PARLOR_HOST_H

#include <stddef.h>
#include <stdint.h>

// Pixel formats of host_frame.
// HOST_NV12 is OpenGL cores' frames, converted on the GPU: the Y plane,
// then the interleaved U and V at half size; stride is the width.
enum { HOST_RGBA = 0, HOST_XRGB8888 = 1, HOST_RGB565 = 2, HOST_0RGB1555 = 3, HOST_NV12 = 4 };

// host_open loads the core. opts are the core's options, as key, value,
// key, value... render_node may be NULL for cores that don't use OpenGL.
int host_open(const char *core, const char *system_dir, const char *save_dir,
              const char *render_node, const char **opts, int nopts, char *err, int errlen);
// host_load loads the game and sets up rendering.
int host_load(const char *rom, char *err, int errlen);
void host_av(double *fps, double *sample_rate, unsigned *w, unsigned *h);
// host_run emulates one frame; 1 when it drew a new one.
int host_run(void);
// host_frame is the last frame: its pixels, stays valid until the next
// host_run. flip is set when its rows run bottom-up.
const uint8_t *host_frame(unsigned *w, unsigned *h, unsigned *stride, int *fmt, int *flip);
// host_audio is the sound since the last call, as interleaved stereo
// frames; host_audio_clear empties it.
const int16_t *host_audio(size_t *frames);
void host_audio_clear(void);
// host_input sets what's held: libretro's joypad buttons as bits, the
// analog sticks (-32767..32767) and the pointer.
void host_input(uint32_t buttons, int16_t lx, int16_t ly, int16_t rx, int16_t ry, int touching,
                int16_t tx, int16_t ty);
// host_set_option changes a core option; the core picks it up on its
// next frame.
void host_set_option(const char *key, const char *value);
// host_set_max_height scales OpenGL frames taller than h down to it
// (0: never), so the game can render at a higher resolution than it's
// streamed at.
void host_set_max_height(unsigned h);
// host_take_convert_ns is the time spent converting and reading back
// OpenGL frames since the last call.
uint64_t host_take_convert_ns(void);
size_t host_state_size(void);
int host_serialize(void *data, size_t size);
int host_unserialize(const void *data, size_t size);
void host_reset(void);
void host_close(void);

#endif
