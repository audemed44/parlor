// Package stream plays a game on the server and streams it to the browser
// over WebRTC: for consoles too heavy to emulate in a phone's browser (the
// 3DS). The game runs on a libretro core (package retro), frames are
// encoded to H.264 and sound to Opus (package media), and the browser
// sends its buttons and touches back over a data channel.
//
// parlor-stream runs beside Parlor. Parlor hands it a game and the
// browser's WebRTC offer; it starts a process for that one game (cores
// keep global state) which answers, plays, and keeps the game's save and
// states in Parlor through Parlor's API, like any other player.
package stream

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// System is what a console needs from the core.
type System struct {
	Core string
	// SaveDir is the folder, under the core's save directory, that holds
	// the game's own saves; it's packed into one save for Parlor.
	SaveDir string
	Options map[string]string
	// Resolution is the core option for the internal resolution, as a
	// multiple of the console's.
	Resolution string
	// DpadToStick tilts the left stick with the D-pad too: 3DS games walk
	// with the Circle Pad.
	DpadToStick bool
}

// Systems are the consoles parlor-stream plays.
var Systems = map[string]System{
	"3ds": {
		Core:    "azahar_libretro.so",
		SaveDir: "Azahar/sdmc",
		Options: map[string]string{
			"citra_graphics_api":           "OpenGL",
			"citra_use_libretro_save_path": "LibRetro Default",
			"citra_use_disk_shader_cache":  "enabled",
			"citra_language_value":         "English",
			"citra_layout_option":          "default",
			"citra_is_new_3ds":             "New 3DS",
		},
		Resolution:  "citra_resolution_factor",
		DpadToStick: true,
	},
}

// Config is parlor-stream's settings, from the environment.
type Config struct {
	// Cores holds the libretro cores.
	Cores string
	// Data keeps what outlives a game: the cores' system files and
	// shader caches. Saves live in Parlor.
	Data string
	// ROMs is the library, read-only, as Parlor sees it.
	ROMs string
	// GPU is the render node games draw on; "" for none.
	GPU string
	// Encoder is "vaapi", "nvenc" or "x264".
	Encoder string
	// Scale is the internal resolution games start at, as a multiple of
	// the console's; MaxHeight scales the stream down to at most that
	// many pixels tall (the game still renders at Scale).
	Scale     int
	MaxHeight int
	// Kbps is the video bitrate; 0 picks one for the frame size.
	Kbps int
	// UDPPort carries every stream; Hosts are the addresses the browser
	// reaches it on (the server's Tailscale or LAN IPs).
	UDPPort int
	Hosts   []string
	// Parlor's API, for saves and states.
	ParlorURL string
	Token     string
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return n
	}
	return fallback
}

// FromEnv reads the settings.
func FromEnv() Config {
	c := Config{
		Cores:     env("PARLOR_STREAM_CORES", "/usr/lib/parlor-stream"),
		Data:      env("PARLOR_STREAM_DATA", "/data"),
		ROMs:      env("PARLOR_ROMS", "/roms"),
		GPU:       env("PARLOR_STREAM_GPU", "auto"),
		Encoder:   env("PARLOR_STREAM_ENCODER", "auto"),
		Scale:     envInt("PARLOR_STREAM_SCALE", 2),
		MaxHeight: envInt("PARLOR_STREAM_MAX_HEIGHT", 1440),
		Kbps:      envInt("PARLOR_STREAM_KBPS", 0),
		UDPPort:   envInt("PARLOR_STREAM_UDP_PORT", 8089),
		ParlorURL: strings.TrimSuffix(env("PARLOR_URL", "http://parlor:8080"), "/"),
		Token:     os.Getenv("PARLOR_TOKEN"),
	}
	for _, h := range strings.Split(os.Getenv("PARLOR_STREAM_HOSTS"), ",") {
		if h = strings.TrimSpace(h); h != "" {
			c.Hosts = append(c.Hosts, h)
		}
	}
	if c.GPU == "auto" {
		c.GPU = pickGPU()
	}
	if c.Encoder == "auto" {
		c.Encoder = pickEncoder(c.GPU)
	}
	return c
}

// driver is the kernel driver behind a render node: amdgpu, i915,
// nvidia, nouveau...
func driver(node string) string {
	link, err := os.Readlink(filepath.Join("/sys/class/drm", filepath.Base(node), "device/driver"))
	if err != nil {
		return ""
	}
	return filepath.Base(link)
}

// pickGPU prefers NVIDIA's driver, then any other with real OpenGL
// (nouveau has none for recent cards).
func pickGPU() string {
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	best, rank := "", 0
	for _, n := range nodes {
		r := 0
		switch driver(n) {
		case "nvidia":
			r = 3
		case "amdgpu", "i915", "xe", "radeon":
			r = 2
		case "nouveau", "":
			r = 0
		default:
			r = 1
		}
		if r > rank {
			best, rank = n, r
		}
	}
	return best
}

func pickEncoder(gpu string) string {
	switch driver(gpu) {
	case "nvidia":
		return "nvenc"
	case "":
		return "x264"
	}
	return "vaapi"
}

// bitrate picks the video bitrate for a frame size at 60 fps.
func bitrate(w, h int) int {
	kbps := w * h * 60 * 8 / 100 / 1000
	return min(max(kbps, 3000), 20000)
}
