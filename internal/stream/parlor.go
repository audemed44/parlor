package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Parlor is Parlor's API, which the session uses like any other player:
// saves and save states live there.
type Parlor struct {
	URL    string
	Token  string
	Client *http.Client
}

// ErrNone is a save or state that isn't there.
var ErrNone = errors.New("not found")

// Save is a save version, as Parlor describes it.
type Save struct {
	ID      int64  `json:"id"`
	Created string `json:"created"`
}

// State is a save state slot, as Parlor describes it.
type State struct {
	ID      int64  `json:"id"`
	GameID  int64  `json:"game_id"`
	Slot    int    `json:"slot"`
	Created string `json:"created"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Device  string `json:"device"`
	Note    string `json:"note"`
	Image   bool   `json:"image"`
}

// Conflict is a save refused because another device saved since.
type Conflict struct{ Latest Save }

func (c *Conflict) Error() string { return "another device saved this game since" }

func (p *Parlor) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.URL+"/api/"+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
		req.ContentLength = int64(len(body))
	}
	c := p.Client
	if c == nil {
		c = http.DefaultClient
	}
	return c.Do(req)
}

func failed(res *http.Response) error {
	var body struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&body)
	if body.Error == "" {
		body.Error = res.Status
	}
	return fmt.Errorf("parlor: %s", body.Error)
}

// LatestSave is the game's latest in-game save and its version.
func (p *Parlor) LatestSave(ctx context.Context, game int64) ([]byte, int64, error) {
	res, err := p.do(ctx, "GET", fmt.Sprintf("games/%d/save", game), nil)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil, 0, ErrNone
	}
	if res.StatusCode != 200 {
		return nil, 0, failed(res)
	}
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, 0, err
	}
	id, _ := strconv.ParseInt(res.Header.Get("X-Save-Id"), 10, 64)
	return data, id, nil
}

// PutSave uploads a save made on top of version base.
func (p *Parlor) PutSave(ctx context.Context, game int64, data []byte, base int64, device string, force bool) (Save, error) {
	q := url.Values{"base": {strconv.FormatInt(base, 10)}, "device": {device}}
	if force {
		q.Set("force", "1")
	}
	res, err := p.do(ctx, "PUT", fmt.Sprintf("games/%d/save?%s", game, q.Encode()), data)
	if err != nil {
		return Save{}, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case 200:
		var v Save
		return v, json.NewDecoder(res.Body).Decode(&v)
	case 409:
		var body struct {
			Latest Save `json:"latest"`
		}
		_ = json.NewDecoder(res.Body).Decode(&body)
		return Save{}, &Conflict{Latest: body.Latest}
	}
	return Save{}, failed(res)
}

// GetState is the state in a slot.
func (p *Parlor) GetState(ctx context.Context, game int64, slot int) ([]byte, error) {
	res, err := p.do(ctx, "GET", fmt.Sprintf("games/%d/states/%d", game, slot), nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil, ErrNone
	}
	if res.StatusCode != 200 {
		return nil, failed(res)
	}
	return io.ReadAll(res.Body)
}

// PutState puts a state in a slot.
func (p *Parlor) PutState(ctx context.Context, game int64, slot int, data []byte, device string, at time.Time) (State, error) {
	q := url.Values{"device": {device}, "at": {at.UTC().Format(time.RFC3339)}}
	res, err := p.do(ctx, "PUT", fmt.Sprintf("games/%d/states/%d?%s", game, slot, q.Encode()), data)
	if err != nil {
		return State{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return State{}, failed(res)
	}
	var v State
	return v, json.NewDecoder(res.Body).Decode(&v)
}
