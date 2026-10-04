package server

import (
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"

	"github.com/audemed44/parlor/internal/patch"
	"github.com/audemed44/parlor/internal/store"
)

// maxPatch is the largest patch accepted; one can hold a whole ROM.
const maxPatch = patch.MaxSize + 1<<20

func (s *Server) patchRoutes(mux *http.ServeMux) {
	// Applies an .ips, .ups or .bps patch to a base ROM and adds the
	// result to the library. Form fields: file (the patch), base (the base
	// ROM's game; 0 finds it by the patch's checksum, for UPS and BPS),
	// title, carry (the game whose save goes over; 0 for none), hide ("1"
	// hides carry), device.
	mux.HandleFunc("POST /api/patch", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxPatch+(64<<10))
		f, _, err := r.FormFile("file")
		if err != nil {
			failure(w, 400, "Choose a patch (.ips, .ups or .bps)")
			return
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			failure(w, 413, "The patch is too large")
			return
		}
		// Patching holds two ROMs in memory; give it back afterwards.
		defer debug.FreeOSMemory()
		info, err := patch.Inspect(data)
		if err != nil {
			failure(w, 400, "That isn't an IPS, UPS or BPS patch")
			return
		}
		baseID, _ := strconv.ParseInt(r.FormValue("base"), 10, 64)
		var source []byte
		if baseID == 0 {
			if !info.Checked() {
				failure(w, 400, "IPS patches don't say which ROM they're for; choose the base ROM")
				return
			}
			baseID, source, err = s.findBase(info)
			if err != nil {
				storeFailure(w, err)
				return
			}
			if baseID == 0 {
				failure(w, 400, "None of the library's ROMs is the one this patch is for. Add the clean base ROM to the library folder and rescan.")
				return
			}
		} else {
			base, err := s.Store.Game(baseID)
			if err != nil {
				storeFailure(w, err)
				return
			}
			if source, err = os.ReadFile(s.Store.ROMFile(s.ROMs, base)); err != nil {
				failure(w, 404, "The base ROM is missing; rescan the library")
				return
			}
		}
		rom, err := patch.Apply(source, data)
		switch {
		case errors.Is(err, patch.ErrWrongROM):
			failure(w, 400, "This patch is for a different ROM; choose its clean base ROM")
			return
		case err != nil:
			failure(w, 400, "Couldn't apply the patch: "+err.Error())
			return
		}
		carry, _ := strconv.ParseInt(r.FormValue("carry"), 10, 64)
		g, err := s.Store.AddPatched(store.NewPatched{
			Title: r.FormValue("title"), ROM: rom, CarryFrom: carry,
			HideOld: r.FormValue("hide") == "1", Device: device(r.FormValue("device")),
		})
		var exists store.ErrExists
		switch {
		case errors.Is(err, store.ErrBadName):
			failure(w, 400, err.Error())
		case errors.As(err, &exists):
			failure(w, 409, "You already have this ROM: "+exists.Title)
		case err != nil:
			storeFailure(w, err)
		default:
			jsonResponse(w, g)
		}
	})
}

// findBase is the library ROM a UPS or BPS patch is for, by its size and
// checksum, with its bytes.
func (s *Server) findBase(info patch.Info) (int64, []byte, error) {
	games, err := s.Store.Games()
	if err != nil {
		return 0, nil, err
	}
	for _, g := range games {
		if g.Missing || g.Size != info.SourceSize {
			continue
		}
		data, err := os.ReadFile(s.Store.ROMFile(s.ROMs, g))
		if err == nil && crc32.ChecksumIEEE(data) == info.SourceCRC {
			return g.ID, data, nil
		}
	}
	return 0, nil, nil
}
