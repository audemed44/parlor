#define GL_GLEXT_PROTOTYPES
#include "host.h"

#include <EGL/egl.h>
#include <EGL/eglext.h>
#include <GL/gl.h>
#include <GL/glext.h>
#include <dlfcn.h>
#include <fcntl.h>
#include <gbm.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "libretro.h"

static struct {
  void *dl;
  void (*init)(void);
  void (*deinit)(void);
  bool (*load_game)(const struct retro_game_info *);
  void (*unload_game)(void);
  void (*run)(void);
  void (*get_av)(struct retro_system_av_info *);
  void (*get_system_info)(struct retro_system_info *);
  size_t (*state_size)(void);
  bool (*serialize)(void *, size_t);
  bool (*unserialize)(const void *, size_t);
  void (*reset)(void);
  void (*set_controller)(unsigned, unsigned);

  char system_dir[1024], save_dir[1024];
  char **opts;
  int nopts;

  // OpenGL, for cores that ask for it.
  bool hw;
  struct retro_hw_render_callback hwcb;
  int fd;
  struct gbm_device *gbm;
  EGLDisplay dpy;
  EGLContext ctx;
  GLuint fbo, tex, rb;
  unsigned fbw, fbh;

  struct retro_system_av_info av;
  enum retro_pixel_format pixfmt;
  uint8_t *frame;
  size_t frame_cap;
  unsigned w, h, stride;
  int fmt, flip;
  bool drew;

  int16_t *audio;
  size_t audio_len, audio_cap;

  // Converting frames to NV12 on the GPU, in a context of our own that
  // shares the core's textures, so the core's OpenGL state is left alone.
  EGLContext conv_ctx;
  GLuint conv_prog[2], conv_fbo[2], conv_tex[2], conv_vao;
  unsigned conv_w, conv_h, max_height;
  bool options_changed;

  uint32_t buttons;
  int16_t analog[2][2];
  int touching;
  int16_t tx, ty;
} H = {.fd = -1, .pixfmt = RETRO_PIXEL_FORMAT_0RGB1555};

static void fail(char *err, int errlen, const char *fmt, ...) {
  va_list ap;
  va_start(ap, fmt);
  vsnprintf(err, errlen, fmt, ap);
  va_end(ap);
}

static void log_cb(enum retro_log_level level, const char *fmt, ...) {
  if (level < RETRO_LOG_WARN) return;
  va_list ap;
  va_start(ap, fmt);
  fputs("[core] ", stderr);
  vfprintf(stderr, fmt, ap);
  va_end(ap);
}

static uintptr_t current_framebuffer(void) { return H.fbo; }

static retro_proc_address_t proc_address(const char *sym) {
  return (retro_proc_address_t)eglGetProcAddress(sym);
}

static const char *option(const char *key) {
  for (int i = 0; i + 1 < H.nopts; i += 2)
    if (!strcmp(H.opts[i], key)) return H.opts[i + 1];
  return NULL;
}

static void set_av(const struct retro_system_av_info *av) { H.av = *av; }

static bool environment(unsigned cmd, void *data) {
  switch (cmd) {
  case RETRO_ENVIRONMENT_GET_LOG_INTERFACE:
    ((struct retro_log_callback *)data)->log = log_cb;
    return true;
  case RETRO_ENVIRONMENT_GET_SYSTEM_DIRECTORY:
  case RETRO_ENVIRONMENT_GET_CORE_ASSETS_DIRECTORY:
    *(const char **)data = H.system_dir;
    return true;
  case RETRO_ENVIRONMENT_GET_SAVE_DIRECTORY:
    *(const char **)data = H.save_dir;
    return true;
  case RETRO_ENVIRONMENT_SET_PIXEL_FORMAT: {
    enum retro_pixel_format f = *(enum retro_pixel_format *)data;
    if (f > RETRO_PIXEL_FORMAT_RGB565) return false;
    H.pixfmt = f;
    return true;
  }
  case RETRO_ENVIRONMENT_GET_PREFERRED_HW_RENDER:
    *(unsigned *)data = RETRO_HW_CONTEXT_OPENGL_CORE;
    return true;
  case RETRO_ENVIRONMENT_SET_HW_RENDER: {
    struct retro_hw_render_callback *cb = data;
    if (cb->context_type != RETRO_HW_CONTEXT_OPENGL_CORE && cb->context_type != RETRO_HW_CONTEXT_OPENGL)
      return false;
    if (!H.dpy) return false;
    cb->get_current_framebuffer = current_framebuffer;
    cb->get_proc_address = proc_address;
    H.hwcb = *cb;
    H.hw = true;
    return true;
  }
  case RETRO_ENVIRONMENT_GET_CORE_OPTIONS_VERSION:
    *(unsigned *)data = 0;
    return true;
  case RETRO_ENVIRONMENT_SET_VARIABLES:
    return true;
  case RETRO_ENVIRONMENT_GET_VARIABLE: {
    struct retro_variable *v = data;
    v->value = option(v->key);
    return v->value != NULL;
  }
  case RETRO_ENVIRONMENT_GET_VARIABLE_UPDATE:
    *(bool *)data = H.options_changed;
    H.options_changed = false;
    return true;
  case RETRO_ENVIRONMENT_SET_GEOMETRY:
    H.av.geometry = *(const struct retro_game_geometry *)data;
    return true;
  case RETRO_ENVIRONMENT_SET_SYSTEM_AV_INFO:
    set_av(data);
    return true;
  case RETRO_ENVIRONMENT_GET_CAN_DUPE:
    *(bool *)data = true;
    return true;
  case RETRO_ENVIRONMENT_GET_INPUT_BITMASKS:
    return true;
  case RETRO_ENVIRONMENT_GET_USERNAME:
    *(const char **)data = "Parlor";
    return true;
  case RETRO_ENVIRONMENT_GET_LANGUAGE:
    *(unsigned *)data = RETRO_LANGUAGE_ENGLISH;
    return true;
  case RETRO_ENVIRONMENT_GET_AUDIO_VIDEO_ENABLE:
    *(int *)data = 3;
    return true;
  case RETRO_ENVIRONMENT_GET_FASTFORWARDING:
    *(bool *)data = false;
    return true;
  case RETRO_ENVIRONMENT_SET_MESSAGE: {
    const struct retro_message *m = data;
    fprintf(stderr, "[core] %s\n", m->msg);
    return true;
  }
  case RETRO_ENVIRONMENT_SET_MESSAGE_EXT: {
    const struct retro_message_ext *m = data;
    fprintf(stderr, "[core] %s\n", m->msg);
    return true;
  }
  case RETRO_ENVIRONMENT_SET_INPUT_DESCRIPTORS:
  case RETRO_ENVIRONMENT_SET_CONTROLLER_INFO:
  case RETRO_ENVIRONMENT_SET_PERFORMANCE_LEVEL:
  case RETRO_ENVIRONMENT_SET_SERIALIZATION_QUIRKS:
  case RETRO_ENVIRONMENT_SET_CORE_OPTIONS_DISPLAY:
  case RETRO_ENVIRONMENT_SET_SUBSYSTEM_INFO:
  case RETRO_ENVIRONMENT_SET_MEMORY_MAPS:
  case RETRO_ENVIRONMENT_SET_SUPPORT_ACHIEVEMENTS:
    return true;
  }
  return false;
}

static bool grow(uint8_t **buf, size_t *cap, size_t need) {
  if (need <= *cap) return true;
  uint8_t *b = realloc(*buf, need);
  if (!b) return false;
  *buf = b;
  *cap = need;
  return true;
}

static const char *conv_vs = "#version 330 core\n"
  "out vec2 uv;\n"
  "void main() {\n"
  "  vec2 p = vec2((gl_VertexID << 1) & 2, gl_VertexID & 2);\n"
  "  uv = p;\n"
  "  gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0);\n"
  "}\n";

// BT.709, limited range, as the encoder labels the stream. The output's
// first row is the top of the game: OpenGL reads back bottom-up.
static const char *conv_fs[2] = {
  "#version 330 core\n"
  "in vec2 uv; out float o;\n"
  "uniform sampler2D tex; uniform vec2 scale; uniform int flip;\n"
  "void main() {\n"
  "  vec2 p = uv; if (flip == 1) p.y = 1.0 - p.y;\n"
  "  vec3 c = texture(tex, p * scale).rgb;\n"
  "  o = 16.0 / 255.0 + dot(c, vec3(0.1826, 0.6142, 0.0620));\n"
  "}\n",
  "#version 330 core\n"
  "in vec2 uv; out vec2 o;\n"
  "uniform sampler2D tex; uniform vec2 scale; uniform int flip;\n"
  "void main() {\n"
  "  vec2 p = uv; if (flip == 1) p.y = 1.0 - p.y;\n"
  "  vec3 c = texture(tex, p * scale).rgb;\n"
  "  o = vec2(128.0 / 255.0 + dot(c, vec3(-0.1006, -0.3386, 0.4392)),\n"
  "           128.0 / 255.0 + dot(c, vec3(0.4392, -0.3989, -0.0403)));\n"
  "}\n",
};

static GLuint shader(GLenum type, const char *src) {
  GLuint s = glCreateShader(type);
  glShaderSource(s, 1, &src, NULL);
  glCompileShader(s);
  GLint ok;
  glGetShaderiv(s, GL_COMPILE_STATUS, &ok);
  if (!ok) {
    char log[512];
    glGetShaderInfoLog(s, sizeof log, NULL, log);
    fprintf(stderr, "[host] shader: %s\n", log);
  }
  return s;
}

static int conv_setup(void) {
  EGLint attrs[] = {EGL_CONTEXT_MAJOR_VERSION, 3, EGL_CONTEXT_MINOR_VERSION, 3,
                    EGL_CONTEXT_OPENGL_PROFILE_MASK, EGL_CONTEXT_OPENGL_CORE_PROFILE_BIT, EGL_NONE};
  H.conv_ctx = eglCreateContext(H.dpy, EGL_NO_CONFIG_KHR, H.ctx, attrs);
  if (H.conv_ctx == EGL_NO_CONTEXT) return -1;
  if (!eglMakeCurrent(H.dpy, EGL_NO_SURFACE, EGL_NO_SURFACE, H.conv_ctx)) return -1;
  GLuint vs = shader(GL_VERTEX_SHADER, conv_vs);
  for (int i = 0; i < 2; i++) {
    GLuint p = glCreateProgram();
    glAttachShader(p, vs);
    glAttachShader(p, shader(GL_FRAGMENT_SHADER, conv_fs[i]));
    glLinkProgram(p);
    GLint ok;
    glGetProgramiv(p, GL_LINK_STATUS, &ok);
    if (!ok) return -1;
    H.conv_prog[i] = p;
  }
  glGenVertexArrays(1, &H.conv_vao);
  glGenFramebuffers(2, H.conv_fbo);
  eglMakeCurrent(H.dpy, EGL_NO_SURFACE, EGL_NO_SURFACE, H.ctx);
  return 0;
}

// convert turns the w×h frame in the core's framebuffer into NV12 of
// ow×oh in H.frame.
static int convert(unsigned w, unsigned h, unsigned ow, unsigned oh) {
  glFlush();
  if (!eglMakeCurrent(H.dpy, EGL_NO_SURFACE, EGL_NO_SURFACE, H.conv_ctx)) return -1;
  if (ow != H.conv_w || oh != H.conv_h) {
    if (H.conv_tex[0]) glDeleteTextures(2, H.conv_tex);
    glGenTextures(2, H.conv_tex);
    GLenum formats[2] = {GL_R8, GL_RG8};
    for (int i = 0; i < 2; i++) {
      glBindTexture(GL_TEXTURE_2D, H.conv_tex[i]);
      glTexStorage2D(GL_TEXTURE_2D, 1, formats[i], ow >> i, oh >> i);
      glBindFramebuffer(GL_FRAMEBUFFER, H.conv_fbo[i]);
      glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, H.conv_tex[i], 0);
    }
    H.conv_w = ow;
    H.conv_h = oh;
  }
  glBindTexture(GL_TEXTURE_2D, H.tex);
  glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
  glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
  glBindVertexArray(H.conv_vao);
  glPixelStorei(GL_PACK_ALIGNMENT, 1);
  uint8_t *out = H.frame;
  for (int i = 0; i < 2; i++) {
    glUseProgram(H.conv_prog[i]);
    glUniform1i(glGetUniformLocation(H.conv_prog[i], "tex"), 0);
    glUniform2f(glGetUniformLocation(H.conv_prog[i], "scale"), (float)w / H.fbw, (float)h / H.fbh);
    glUniform1i(glGetUniformLocation(H.conv_prog[i], "flip"), H.hwcb.bottom_left_origin ? 1 : 0);
    glBindFramebuffer(GL_FRAMEBUFFER, H.conv_fbo[i]);
    glViewport(0, 0, ow >> i, oh >> i);
    glDrawArrays(GL_TRIANGLES, 0, 3);
    glReadPixels(0, 0, ow >> i, oh >> i, i ? GL_RG : GL_RED, GL_UNSIGNED_BYTE, out);
    out += (size_t)ow * oh;
  }
  eglMakeCurrent(H.dpy, EGL_NO_SURFACE, EGL_NO_SURFACE, H.ctx);
  return 0;
}

static void video_refresh(const void *data, unsigned w, unsigned h, size_t pitch) {
  if (!data || !w || !h) return; // a repeat of the last frame
  if (data == RETRO_HW_FRAME_BUFFER_VALID) {
    unsigned ow = w & ~1u, oh = h & ~1u;
    if (H.max_height && oh > H.max_height) {
      oh = H.max_height & ~1u;
      ow = ((unsigned)((double)w * oh / h + 1)) & ~1u;
    }
    if (!grow(&H.frame, &H.frame_cap, (size_t)ow * oh * 3 / 2)) return;
    if (convert(w, h, ow, oh) < 0) return;
    H.fmt = HOST_NV12;
    H.stride = ow;
    H.flip = 0;
    w = ow;
    h = oh;
  } else {
    unsigned bpp = H.pixfmt == RETRO_PIXEL_FORMAT_XRGB8888 ? 4 : 2;
    if (!grow(&H.frame, &H.frame_cap, (size_t)w * h * bpp)) return;
    for (unsigned y = 0; y < h; y++)
      memcpy(H.frame + (size_t)y * w * bpp, (const uint8_t *)data + y * pitch, (size_t)w * bpp);
    H.fmt = H.pixfmt == RETRO_PIXEL_FORMAT_XRGB8888 ? HOST_XRGB8888
            : H.pixfmt == RETRO_PIXEL_FORMAT_RGB565 ? HOST_RGB565
                                                      : HOST_0RGB1555;
    H.stride = w * bpp;
    H.flip = 0;
  }
  H.w = w;
  H.h = h;
  H.drew = true;
}

static size_t audio_batch(const int16_t *data, size_t frames) {
  size_t need = (H.audio_len + frames) * 2;
  if (need > H.audio_cap) {
    size_t cap = need * 2;
    int16_t *a = realloc(H.audio, cap * sizeof(int16_t));
    if (!a) return frames;
    H.audio = a;
    H.audio_cap = cap;
  }
  memcpy(H.audio + H.audio_len * 2, data, frames * 2 * sizeof(int16_t));
  H.audio_len += frames;
  return frames;
}

static void audio_sample(int16_t l, int16_t r) {
  int16_t s[2] = {l, r};
  audio_batch(s, 1);
}

static void input_poll(void) {}

static int16_t input_state(unsigned port, unsigned device, unsigned index, unsigned id) {
  if (port) return 0;
  switch (device) {
  case RETRO_DEVICE_JOYPAD:
    if (id == RETRO_DEVICE_ID_JOYPAD_MASK) return (int16_t)H.buttons;
    return id < 16 ? (H.buttons >> id) & 1 : 0;
  case RETRO_DEVICE_ANALOG:
    if (index > 1 || id > 1) return 0;
    return H.analog[index][id];
  case RETRO_DEVICE_POINTER:
    switch (id) {
    case RETRO_DEVICE_ID_POINTER_X:
      return H.tx;
    case RETRO_DEVICE_ID_POINTER_Y:
      return H.ty;
    case RETRO_DEVICE_ID_POINTER_PRESSED:
      return H.touching;
    case RETRO_DEVICE_ID_POINTER_COUNT:
      return H.touching ? 1 : 0;
    }
  }
  return 0;
}

// egl_display opens EGL on the render node: through EGL's device list
// (what NVIDIA's driver offers headless), or else through GBM (Mesa).
static int egl_display(const char *node) {
  PFNEGLQUERYDEVICESEXTPROC query_devices =
      (PFNEGLQUERYDEVICESEXTPROC)eglGetProcAddress("eglQueryDevicesEXT");
  PFNEGLQUERYDEVICESTRINGEXTPROC device_string =
      (PFNEGLQUERYDEVICESTRINGEXTPROC)eglGetProcAddress("eglQueryDeviceStringEXT");
  PFNEGLGETPLATFORMDISPLAYEXTPROC get_display =
      (PFNEGLGETPLATFORMDISPLAYEXTPROC)eglGetProcAddress("eglGetPlatformDisplayEXT");
  if (!get_display) return -1;
  EGLint major, minor;
  if (query_devices && device_string) {
    EGLDeviceEXT devices[16];
    EGLint n = 0;
    if (query_devices(16, devices, &n)) {
      for (EGLint i = 0; i < n; i++) {
        const char *file = device_string(devices[i], EGL_DRM_RENDER_NODE_FILE_EXT);
        if (!file || strcmp(file, node)) continue;
        H.dpy = get_display(EGL_PLATFORM_DEVICE_EXT, devices[i], NULL);
        if (H.dpy != EGL_NO_DISPLAY && eglInitialize(H.dpy, &major, &minor)) return 0;
      }
    }
  }
  H.fd = open(node, O_RDWR | O_CLOEXEC);
  if (H.fd < 0) return -1;
  H.gbm = gbm_create_device(H.fd);
  H.dpy = get_display(EGL_PLATFORM_GBM_KHR, H.gbm, NULL);
  if (H.dpy == EGL_NO_DISPLAY || !eglInitialize(H.dpy, &major, &minor)) return -1;
  return 0;
}

#define SYM(field, name)                                                  \
  if (!(*(void **)&H.field = dlsym(H.dl, name))) {                        \
    fail(err, errlen, "the core has no %s", name);                       \
    return -1;                                                            \
  }

int host_open(const char *core, const char *system_dir, const char *save_dir,
              const char *render_node, const char **opts, int nopts, char *err, int errlen) {
  snprintf(H.system_dir, sizeof H.system_dir, "%s", system_dir);
  snprintf(H.save_dir, sizeof H.save_dir, "%s", save_dir);
  H.opts = calloc(nopts + 1, sizeof(char *));
  for (int i = 0; i < nopts; i++) H.opts[i] = strdup(opts[i]);
  H.nopts = nopts;

  if (render_node && *render_node) {
    if (egl_display(render_node) < 0) {
      fail(err, errlen, "no EGL on %s", render_node);
      return -1;
    }
    eglBindAPI(EGL_OPENGL_API);
  }

  H.dl = dlopen(core, RTLD_NOW | RTLD_LOCAL);
  if (!H.dl) {
    fail(err, errlen, "%s", dlerror());
    return -1;
  }
  SYM(init, "retro_init")
  SYM(deinit, "retro_deinit")
  SYM(load_game, "retro_load_game")
  SYM(unload_game, "retro_unload_game")
  SYM(run, "retro_run")
  SYM(get_av, "retro_get_system_av_info")
  SYM(get_system_info, "retro_get_system_info")
  SYM(state_size, "retro_serialize_size")
  SYM(serialize, "retro_serialize")
  SYM(unserialize, "retro_unserialize")
  SYM(reset, "retro_reset")
  SYM(set_controller, "retro_set_controller_port_device")

  void (*set_environment)(retro_environment_t) = dlsym(H.dl, "retro_set_environment");
  void (*set_video)(retro_video_refresh_t) = dlsym(H.dl, "retro_set_video_refresh");
  void (*set_audio)(retro_audio_sample_t) = dlsym(H.dl, "retro_set_audio_sample");
  void (*set_audio_batch)(retro_audio_sample_batch_t) = dlsym(H.dl, "retro_set_audio_sample_batch");
  void (*set_poll)(retro_input_poll_t) = dlsym(H.dl, "retro_set_input_poll");
  void (*set_state)(retro_input_state_t) = dlsym(H.dl, "retro_set_input_state");
  if (!set_environment || !set_video || !set_audio || !set_audio_batch || !set_poll || !set_state) {
    fail(err, errlen, "not a libretro core");
    return -1;
  }
  set_environment(environment);
  set_video(video_refresh);
  set_audio(audio_sample);
  set_audio_batch(audio_batch);
  set_poll(input_poll);
  set_state(input_state);
  H.init();
  return 0;
}

// framebuffer makes the core's render target, as big as the game's
// largest frame.
static int framebuffer(unsigned w, unsigned h) {
  if (H.fbo && w <= H.fbw && h <= H.fbh) return 0;
  if (H.fbo) {
    glDeleteFramebuffers(1, &H.fbo);
    glDeleteTextures(1, &H.tex);
    glDeleteRenderbuffers(1, &H.rb);
  }
  H.fbw = w;
  H.fbh = h;
  glGenTextures(1, &H.tex);
  glBindTexture(GL_TEXTURE_2D, H.tex);
  glTexStorage2D(GL_TEXTURE_2D, 1, GL_RGBA8, w, h);
  glGenRenderbuffers(1, &H.rb);
  glBindRenderbuffer(GL_RENDERBUFFER, H.rb);
  glRenderbufferStorage(GL_RENDERBUFFER, GL_DEPTH24_STENCIL8, w, h);
  glGenFramebuffers(1, &H.fbo);
  glBindFramebuffer(GL_FRAMEBUFFER, H.fbo);
  glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, H.tex, 0);
  glFramebufferRenderbuffer(GL_FRAMEBUFFER, GL_DEPTH_STENCIL_ATTACHMENT, GL_RENDERBUFFER, H.rb);
  return glCheckFramebufferStatus(GL_FRAMEBUFFER) == GL_FRAMEBUFFER_COMPLETE ? 0 : -1;
}

int host_load(const char *rom, char *err, int errlen) {
  struct retro_system_info info = {0};
  H.get_system_info(&info);
  struct retro_game_info game = {.path = rom};
  void *data = NULL;
  if (!info.need_fullpath) {
    FILE *f = fopen(rom, "rb");
    if (!f) {
      fail(err, errlen, "can't open the ROM");
      return -1;
    }
    fseek(f, 0, SEEK_END);
    long n = ftell(f);
    rewind(f);
    data = malloc(n);
    if (!data || fread(data, 1, n, f) != (size_t)n) {
      fclose(f);
      free(data);
      fail(err, errlen, "can't read the ROM");
      return -1;
    }
    fclose(f);
    game.data = data;
    game.size = n;
  }
  bool ok = H.load_game(&game);
  free(data);
  if (!ok) {
    fail(err, errlen, "the core couldn't load the game");
    return -1;
  }
  H.set_controller(0, RETRO_DEVICE_JOYPAD);
  H.get_av(&H.av);
  if (H.hw) {
    EGLint attrs[] = {EGL_CONTEXT_MAJOR_VERSION, H.hwcb.version_major ? (EGLint)H.hwcb.version_major : 3,
                      EGL_CONTEXT_MINOR_VERSION, (EGLint)H.hwcb.version_minor,
                      EGL_CONTEXT_OPENGL_PROFILE_MASK,
                      H.hwcb.context_type == RETRO_HW_CONTEXT_OPENGL_CORE
                          ? EGL_CONTEXT_OPENGL_CORE_PROFILE_BIT
                          : EGL_CONTEXT_OPENGL_COMPATIBILITY_PROFILE_BIT,
                      EGL_NONE};
    H.ctx = eglCreateContext(H.dpy, EGL_NO_CONFIG_KHR, EGL_NO_CONTEXT, attrs);
    if (H.ctx == EGL_NO_CONTEXT || !eglMakeCurrent(H.dpy, EGL_NO_SURFACE, EGL_NO_SURFACE, H.ctx)) {
      fail(err, errlen, "can't create an OpenGL %u.%u context (EGL 0x%x)", H.hwcb.version_major,
           H.hwcb.version_minor, eglGetError());
      return -1;
    }
    fprintf(stderr, "[host] OpenGL %s on %s\n", glGetString(GL_VERSION), glGetString(GL_RENDERER));
    unsigned w = H.av.geometry.base_width, h = H.av.geometry.base_height;
    if (framebuffer(w, h) < 0) {
      fail(err, errlen, "can't make a %ux%u framebuffer", w, h);
      return -1;
    }
    if (conv_setup() < 0) {
      fail(err, errlen, "can't set up the frame conversion");
      return -1;
    }
    if (H.hwcb.context_reset) H.hwcb.context_reset();
  }
  return 0;
}

void host_av(double *fps, double *sample_rate, unsigned *w, unsigned *h) {
  *fps = H.av.timing.fps;
  *sample_rate = H.av.timing.sample_rate;
  *w = H.av.geometry.base_width;
  *h = H.av.geometry.base_height;
}

int host_run(void) {
  H.drew = false;
  if (H.hw) framebuffer(H.av.geometry.base_width, H.av.geometry.base_height);
  H.run();
  return H.drew;
}

const uint8_t *host_frame(unsigned *w, unsigned *h, unsigned *stride, int *fmt, int *flip) {
  *w = H.w;
  *h = H.h;
  *stride = H.stride;
  *fmt = H.fmt;
  *flip = H.flip;
  return H.frame;
}

const int16_t *host_audio(size_t *frames) {
  *frames = H.audio_len;
  return H.audio;
}

void host_audio_clear(void) { H.audio_len = 0; }

void host_input(uint32_t buttons, int16_t lx, int16_t ly, int16_t rx, int16_t ry, int touching,
                int16_t tx, int16_t ty) {
  H.buttons = buttons;
  H.analog[0][0] = lx;
  H.analog[0][1] = ly;
  H.analog[1][0] = rx;
  H.analog[1][1] = ry;
  H.touching = touching;
  H.tx = tx;
  H.ty = ty;
}

void host_set_option(const char *key, const char *value) {
  for (int i = 0; i + 1 < H.nopts; i += 2) {
    if (!strcmp(H.opts[i], key)) {
      free(H.opts[i + 1]);
      H.opts[i + 1] = strdup(value);
      H.options_changed = true;
      return;
    }
  }
  H.opts = realloc(H.opts, (H.nopts + 3) * sizeof(char *));
  H.opts[H.nopts++] = strdup(key);
  H.opts[H.nopts++] = strdup(value);
  H.options_changed = true;
}

void host_set_max_height(unsigned h) { H.max_height = h; }

size_t host_state_size(void) { return H.state_size(); }
int host_serialize(void *data, size_t size) { return H.serialize(data, size) ? 0 : -1; }
int host_unserialize(const void *data, size_t size) { return H.unserialize(data, size) ? 0 : -1; }
void host_reset(void) { H.reset(); }

void host_close(void) {
  if (!H.dl) return;
  if (H.hw && H.hwcb.context_destroy) H.hwcb.context_destroy();
  H.unload_game();
  H.deinit();
}
