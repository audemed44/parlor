#include "encode.h"

#include <libavcodec/avcodec.h>
#include <libavutil/audio_fifo.h>
#include <libavutil/hwcontext.h>
#include <libavutil/imgutils.h>
#include <libavutil/opt.h>
#include <libswresample/swresample.h>
#include <libswscale/swscale.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// The host's pixel formats (retro/host.h), in FFmpeg's terms. RGBA is
// bytes in that order; XRGB8888 is a native-endian word, so B, G, R, X.
static enum AVPixelFormat pixfmt(int fmt) {
  switch (fmt) {
  case 0:
    return AV_PIX_FMT_RGBA;
  case 1:
    return AV_PIX_FMT_BGR0;
  case 2:
    return AV_PIX_FMT_RGB565LE;
  case 3:
    return AV_PIX_FMT_RGB555LE;
  default:
    return AV_PIX_FMT_NV12;
  }
}

static void averr(char *err, int errlen, const char *what, int code) {
  char msg[128];
  av_strerror(code, msg, sizeof msg);
  snprintf(err, errlen, "%s: %s", what, msg);
}

struct venc {
  AVCodecContext *ctx;
  AVBufferRef *device, *frames;
  AVFrame *sw, *hw;
  AVPacket *pkt;
  struct SwsContext *sws;
  int sws_fmt;
  int64_t pts;
  uint8_t *out;
  size_t out_len, out_cap;
};

venc *venc_open(const char *kind, const char *device, int w, int h, int fps, int kbps, char *err,
                int errlen) {
  venc *e = calloc(1, sizeof *e);
  int vaapi = !strcmp(kind, "vaapi");
  const char *name = vaapi ? "h264_vaapi" : !strcmp(kind, "nvenc") ? "h264_nvenc" : "libx264";
  const AVCodec *codec = avcodec_find_encoder_by_name(name);
  if (!codec) {
    snprintf(err, errlen, "FFmpeg has no %s encoder", name);
    goto fail;
  }
  int r;
  if (vaapi && (r = av_hwdevice_ctx_create(&e->device, AV_HWDEVICE_TYPE_VAAPI, device, NULL, 0)) < 0) {
    averr(err, errlen, "VAAPI", r);
    goto fail;
  }
  e->ctx = avcodec_alloc_context3(codec);
  AVCodecContext *c = e->ctx;
  c->width = w;
  c->height = h;
  c->time_base = (AVRational){1, fps};
  c->framerate = (AVRational){fps, 1};
  c->bit_rate = (int64_t)kbps * 1000;
  c->rc_max_rate = c->bit_rate;
  c->rc_buffer_size = (int)(c->bit_rate / fps * 2); // two frames: no long bursts
  // A keyframe every 10 s, besides the ones the browser asks for.
  c->gop_size = fps * 10;
  c->max_b_frames = 0;
  c->profile = FF_PROFILE_H264_CONSTRAINED_BASELINE;
  c->flags |= AV_CODEC_FLAG_LOW_DELAY;
  // Labelled BT.709, limited range: what the GPU conversion writes.
  c->colorspace = AVCOL_SPC_BT709;
  c->color_primaries = AVCOL_PRI_BT709;
  c->color_trc = AVCOL_TRC_BT709;
  c->color_range = AVCOL_RANGE_MPEG;
  c->pix_fmt = vaapi ? AV_PIX_FMT_VAAPI : !strcmp(kind, "nvenc") ? AV_PIX_FMT_NV12 : AV_PIX_FMT_YUV420P;
  if (vaapi) {
    av_opt_set(c->priv_data, "rc_mode", "CBR", 0);
    av_opt_set_int(c->priv_data, "async_depth", 1, 0);
    e->frames = av_hwframe_ctx_alloc(e->device);
    AVHWFramesContext *f = (AVHWFramesContext *)e->frames->data;
    f->format = AV_PIX_FMT_VAAPI;
    f->sw_format = AV_PIX_FMT_NV12;
    f->width = w;
    f->height = h;
    f->initial_pool_size = 4;
    if ((r = av_hwframe_ctx_init(e->frames)) < 0) {
      averr(err, errlen, "VAAPI frames", r);
      goto fail;
    }
    c->hw_frames_ctx = av_buffer_ref(e->frames);
  } else if (!strcmp(kind, "nvenc")) {
    av_opt_set(c->priv_data, "preset", "p1", 0);
    av_opt_set(c->priv_data, "tune", "ull", 0);
    av_opt_set(c->priv_data, "rc", "cbr", 0);
    av_opt_set_int(c->priv_data, "zerolatency", 1, 0);
    av_opt_set_int(c->priv_data, "forced-idr", 1, 0);
    av_opt_set_int(c->priv_data, "delay", 0, 0);
  } else {
    av_opt_set(c->priv_data, "preset", "ultrafast", 0);
    av_opt_set(c->priv_data, "tune", "zerolatency", 0);
    av_opt_set_int(c->priv_data, "forced-idr", 1, 0);
  }
  if ((r = avcodec_open2(c, codec, NULL)) < 0) {
    averr(err, errlen, name, r);
    goto fail;
  }
  e->sw = av_frame_alloc();
  e->sw->format = vaapi || !strcmp(kind, "nvenc") ? AV_PIX_FMT_NV12 : AV_PIX_FMT_YUV420P;
  e->sw->width = w;
  e->sw->height = h;
  if (av_frame_get_buffer(e->sw, 0) < 0) {
    snprintf(err, errlen, "out of memory");
    goto fail;
  }
  e->hw = av_frame_alloc();
  e->pkt = av_packet_alloc();
  e->sws_fmt = -1;
  return e;
fail:
  venc_close(e);
  return NULL;
}

static int append(venc *e, const AVPacket *p) {
  if (e->out_len + p->size > e->out_cap) {
    size_t cap = (e->out_len + p->size) * 2;
    uint8_t *b = realloc(e->out, cap);
    if (!b) return -1;
    e->out = b;
    e->out_cap = cap;
  }
  memcpy(e->out + e->out_len, p->data, p->size);
  e->out_len += p->size;
  return 0;
}

int venc_encode(venc *e, const uint8_t *pix, int fmt, int stride, int flip, int keyframe,
                const uint8_t **out) {
  AVCodecContext *c = e->ctx;
  if (av_frame_make_writable(e->sw) < 0) return -1;
  const uint8_t *src[4] = {pix};
  int src_stride[4] = {stride};
  if (fmt == 4) { // NV12 from the GPU, already the right way up
    src[1] = pix + (size_t)stride * c->height;
    src_stride[1] = stride;
  } else if (flip) { // OpenGL's rows run bottom-up
    src[0] = pix + (size_t)stride * (c->height - 1);
    src_stride[0] = -stride;
  }
  if (fmt == 4 && e->sw->format == AV_PIX_FMT_NV12) {
    av_image_copy(e->sw->data, e->sw->linesize, src, src_stride, AV_PIX_FMT_NV12, c->width, c->height);
  } else {
    if (fmt != e->sws_fmt) {
      sws_freeContext(e->sws);
      e->sws = sws_getContext(c->width, c->height, pixfmt(fmt), c->width, c->height, e->sw->format,
                              SWS_POINT, NULL, NULL, NULL);
      e->sws_fmt = fmt;
      if (!e->sws) return -1;
      if (fmt != 4) { // RGB in: write BT.709, limited range, like the GPU does
        const int *bt709 = sws_getCoefficients(SWS_CS_ITU709);
        sws_setColorspaceDetails(e->sws, bt709, 1, bt709, 0, 0, 1 << 16, 1 << 16);
      }
    }
    sws_scale(e->sws, src, src_stride, 0, c->height, e->sw->data, e->sw->linesize);
  }

  AVFrame *frame = e->sw;
  if (e->frames) {
    av_frame_unref(e->hw);
    if (av_hwframe_get_buffer(e->frames, e->hw, 0) < 0) return -1;
    if (av_hwframe_transfer_data(e->hw, e->sw, 0) < 0) return -1;
    frame = e->hw;
  }
  frame->pts = e->pts++;
  frame->pict_type = keyframe ? AV_PICTURE_TYPE_I : AV_PICTURE_TYPE_NONE;
  if (keyframe)
    frame->flags |= AV_FRAME_FLAG_KEY;
  else
    frame->flags &= ~AV_FRAME_FLAG_KEY;
  if (avcodec_send_frame(c, frame) < 0) return -1;
  e->out_len = 0;
  for (;;) {
    int r = avcodec_receive_packet(c, e->pkt);
    if (r == AVERROR(EAGAIN) || r == AVERROR_EOF) break;
    if (r < 0) return -1;
    int ok = append(e, e->pkt);
    av_packet_unref(e->pkt);
    if (ok < 0) return -1;
  }
  *out = e->out;
  return (int)e->out_len;
}

void venc_close(venc *e) {
  if (!e) return;
  avcodec_free_context(&e->ctx);
  av_frame_free(&e->sw);
  av_frame_free(&e->hw);
  av_packet_free(&e->pkt);
  av_buffer_unref(&e->frames);
  av_buffer_unref(&e->device);
  sws_freeContext(e->sws);
  free(e->out);
  free(e);
}

struct aenc {
  AVCodecContext *ctx;
  SwrContext *swr;
  AVAudioFifo *fifo;
  AVFrame *frame;
  AVPacket *pkt;
  int in_rate;
  int64_t pts;
  uint8_t *conv;
  int conv_cap;
  uint8_t *out;
  int out_cap;
};

aenc *aenc_open(int in_rate, int kbps, char *err, int errlen) {
  aenc *e = calloc(1, sizeof *e);
  const AVCodec *codec = avcodec_find_encoder_by_name("libopus");
  if (!codec) {
    snprintf(err, errlen, "FFmpeg has no libopus encoder");
    goto fail;
  }
  e->ctx = avcodec_alloc_context3(codec);
  AVCodecContext *c = e->ctx;
  c->sample_rate = 48000;
  c->sample_fmt = AV_SAMPLE_FMT_S16;
  c->bit_rate = (int64_t)kbps * 1000;
  c->time_base = (AVRational){1, 48000};
  AVChannelLayout stereo = AV_CHANNEL_LAYOUT_STEREO;
  av_channel_layout_copy(&c->ch_layout, &stereo);
  av_opt_set(c->priv_data, "application", "lowdelay", 0);
  av_opt_set(c->priv_data, "frame_duration", "20", 0);
  int r;
  if ((r = avcodec_open2(c, codec, NULL)) < 0) {
    averr(err, errlen, "libopus", r);
    goto fail;
  }
  if ((r = swr_alloc_set_opts2(&e->swr, &stereo, AV_SAMPLE_FMT_S16, 48000, &stereo, AV_SAMPLE_FMT_S16,
                               in_rate, 0, NULL)) < 0 ||
      (r = swr_init(e->swr)) < 0) {
    averr(err, errlen, "resampler", r);
    goto fail;
  }
  e->in_rate = in_rate;
  e->fifo = av_audio_fifo_alloc(AV_SAMPLE_FMT_S16, 2, 48000);
  e->frame = av_frame_alloc();
  e->frame->nb_samples = c->frame_size;
  e->frame->format = AV_SAMPLE_FMT_S16;
  av_channel_layout_copy(&e->frame->ch_layout, &stereo);
  e->frame->sample_rate = 48000;
  if (av_frame_get_buffer(e->frame, 0) < 0) goto fail;
  e->pkt = av_packet_alloc();
  return e;
fail:
  aenc_close(e);
  return NULL;
}

int aenc_push(aenc *e, const int16_t *samples, int frames) {
  int max = swr_get_out_samples(e->swr, frames);
  if (max * 4 > e->conv_cap) {
    e->conv_cap = max * 4;
    e->conv = realloc(e->conv, e->conv_cap);
  }
  const uint8_t *in[1] = {(const uint8_t *)samples};
  uint8_t *outp[1] = {e->conv};
  int n = swr_convert(e->swr, outp, max, in, frames);
  if (n < 0) return -1;
  // Don't let sound pile up if nobody takes it.
  if (av_audio_fifo_size(e->fifo) > 48000) av_audio_fifo_drain(e->fifo, av_audio_fifo_size(e->fifo) - 4800);
  return av_audio_fifo_write(e->fifo, (void **)outp, n) < n ? -1 : 0;
}

int aenc_next(aenc *e, const uint8_t **out) {
  for (;;) {
    int r = avcodec_receive_packet(e->ctx, e->pkt);
    if (r == 0) {
      if (e->pkt->size > e->out_cap) {
        e->out_cap = e->pkt->size * 2;
        e->out = realloc(e->out, e->out_cap);
      }
      int n = e->pkt->size;
      memcpy(e->out, e->pkt->data, n);
      av_packet_unref(e->pkt);
      *out = e->out;
      return n;
    }
    if (r != AVERROR(EAGAIN)) return r == AVERROR_EOF ? 0 : -1;
    int size = e->ctx->frame_size;
    if (av_audio_fifo_size(e->fifo) < size) return 0;
    if (av_frame_make_writable(e->frame) < 0) return -1;
    av_audio_fifo_read(e->fifo, (void **)e->frame->data, size);
    e->frame->pts = e->pts;
    e->pts += size;
    if (avcodec_send_frame(e->ctx, e->frame) < 0) return -1;
  }
}

void aenc_close(aenc *e) {
  if (!e) return;
  avcodec_free_context(&e->ctx);
  swr_free(&e->swr);
  if (e->fifo) av_audio_fifo_free(e->fifo);
  av_frame_free(&e->frame);
  av_packet_free(&e->pkt);
  free(e->conv);
  free(e->out);
  free(e);
}
