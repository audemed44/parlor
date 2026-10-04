// H.264 and Opus encoding for WebRTC, through FFmpeg's libraries.
#ifndef PARLOR_ENCODE_H
#define PARLOR_ENCODE_H

#include <stddef.h>
#include <stdint.h>

typedef struct venc venc;
typedef struct aenc aenc;

// venc_open opens an H.264 encoder: kind is "vaapi" (device is the render
// node), "nvenc" or "x264". The stream is Constrained Baseline, with no
// B-frames, for every browser's WebRTC.
venc *venc_open(const char *kind, const char *device, int w, int h, int fps, int kbps, char *err,
                int errlen);
// venc_encode encodes a frame (the host's pixel formats); the result is
// Annex B, in *out, valid until the next call. 0 for no output yet, -1 for
// an error.
int venc_encode(venc *e, const uint8_t *pix, int fmt, int stride, int flip, int keyframe,
                const uint8_t **out);
void venc_close(venc *e);

// aenc_open opens an Opus encoder (48 kHz stereo, 20 ms packets) for
// stereo 16-bit sound at in_rate.
aenc *aenc_open(int in_rate, int kbps, char *err, int errlen);
// aenc_push adds sound; aenc_next takes the next finished packet (its
// length, 0 for none), valid until the next call.
int aenc_push(aenc *e, const int16_t *samples, int frames);
int aenc_next(aenc *e, const uint8_t **out);
void aenc_close(aenc *e);

#endif
