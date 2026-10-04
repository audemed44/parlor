package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/audemed44/parlor/internal/library"
	"github.com/audemed44/parlor/internal/store"
)

// The Foyer widget format, version 1: see Foyer's docs/app-widgets.md.
type widget struct {
	Version     int          `json:"version"`
	Stats       []widgetStat `json:"stats"`
	ItemsTitle  string       `json:"items_title,omitempty"`
	ItemsLayout string       `json:"items_layout,omitempty"`
	Items       []widgetItem `json:"items"`
}

type widgetStat struct {
	Label   string `json:"label"`
	Value   string `json:"value"`
	Unit    string `json:"unit,omitempty"`
	Caption string `json:"caption,omitempty"`
	Tone    string `json:"tone,omitempty"`
}

type widgetItem struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Image    string `json:"image,omitempty"`
	URL      string `json:"url"`
	Caption  string `json:"caption,omitempty"`
}

// widgetGames is how many recently played games the widget lists.
const widgetGames = 4

func (s *Server) foyerRoutes(mux *http.ServeMux) {
	// Foyer's widget: what you played last, with a link straight into it
	// ("Continue: Unbound, 2 h ago"), and a few more.
	mux.HandleFunc("GET /api/foyer/widget", func(w http.ResponseWriter, r *http.Request) {
		games, err := s.Store.Games()
		if err != nil {
			storeFailure(w, err)
			return
		}
		jsonResponse(w, buildWidget(games, time.Now()))
	})
}

func buildWidget(games []store.Game, now time.Time) widget {
	out := widget{Version: 1, ItemsTitle: "Continue", ItemsLayout: "list", Items: []widgetItem{}}
	var count int
	var played int64
	var lastSave string
	for _, g := range games {
		if !g.Missing && !g.Hidden {
			count++
		}
		played += g.PlaySeconds
		if g.Save != nil && g.Save.Created > lastSave {
			lastSave = g.Save.Created
		}
	}
	out.Stats = []widgetStat{
		{Label: "Games", Value: strconv.Itoa(count)},
		{Label: "Played", Value: strconv.FormatInt(played/3600, 10), Unit: "h"},
	}
	if lastSave != "" {
		out.Stats = append(out.Stats, widgetStat{Label: "Last save", Value: ago(lastSave, now)})
	}
	// Games are listed most recently played first.
	for _, g := range games {
		if len(out.Items) == widgetGames || g.LastPlayed == "" {
			break
		}
		if g.Missing || g.Hidden {
			continue
		}
		it := widgetItem{
			Title:    g.Title,
			Subtitle: fmt.Sprintf("%s · %s · %s played", library.ShortName(g.Platform), ago(g.LastPlayed, now), hours(g.PlaySeconds)),
			URL:      playURL(g),
			Caption:  "Play",
		}
		if len(out.Items) == 0 {
			it.Title = "Continue: " + g.Title
		}
		if g.Cover != "" {
			it.Image = fmt.Sprintf("/api/games/%d/cover?v=%s", g.ID, g.Cover)
		}
		out.Items = append(out.Items, it)
	}
	return out
}

// ago is a short relative time, like the app's: "just now", "5 min ago",
// "2 h ago", "yesterday", "4 days ago", "12 Sep".
func ago(stamp string, now time.Time) string {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", stamp)
	if err != nil {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
	return t.Local().Format("2 Jan")
}

// hours formats play time: "45 min", "12 h".
func hours(seconds int64) string {
	if seconds < 3600 {
		return fmt.Sprintf("%d min", seconds/60)
	}
	return fmt.Sprintf("%d h", seconds/3600)
}

// playURL opens a game: games other than the GBA's play on /play, the
// page whose content policy EmulatorJS's cores need.
func playURL(g store.Game) string {
	id := strconv.FormatInt(g.ID, 10)
	if g.Platform != "gba" {
		return "/play#/play/" + id
	}
	return "/#/play/" + id
}
