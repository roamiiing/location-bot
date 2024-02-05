package main

import (
	"context"
	"fmt"
	"github.com/davidbyttow/govips/v2/vips"
	"golang.org/x/time/rate"
	"io"
	"net/http"
	"os"
	"time"
)

const tileSize = 512
const defaultZoom = 4
const trimThreshold = 6

type dimensions struct {
	width, height int
}

type tileConfig struct {
	x, y int
	url  string
}

type tileData struct {
	x, y int
	data []byte
}

type RLHTTPClient struct {
	client *http.Client
	rl     *rate.Limiter
}

func (c *RLHTTPClient) Do(req *http.Request) (*http.Response, error) {
	ctx := context.Background()
	err := c.rl.Wait(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func NewClient(rl *rate.Limiter) *RLHTTPClient {
	c := &RLHTTPClient{
		client: http.DefaultClient,
		rl:     rl,
	}

	return c
}

func makePanoUrl(panoId string, zoom, x, y int) string {
	return fmt.Sprintf(
		"https://cbk0.google.com/cbk?output=tile&panoid=%s&zoom=%d&x=%d&y=%d",
		panoId, zoom, x, y,
	)
}

func fetchTile(config tileConfig, rlClient *RLHTTPClient) ([]byte, error) {
	req, err := http.NewRequest("GET", config.url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := rlClient.Do(req)
	if err != nil {
		return nil, err
	}

	return io.ReadAll(resp.Body)
}

func getDimensionsFromZoom(zoom int) dimensions {
	return dimensions{
		width:  1 << zoom,
		height: 1 << (zoom - 1),
	}
}

func getTilesConfig(panoId string, zoom int) []tileConfig {
	dim := getDimensionsFromZoom(zoom)
	tiles := make([]tileConfig, 0, dim.width*dim.height)

	for x := 0; x < dim.width; x++ {
		for y := 0; y < dim.height; y++ {
			tiles = append(tiles, tileConfig{
				x:   x,
				y:   y,
				url: makePanoUrl(panoId, zoom, x, y),
			})
		}
	}

	return tiles
}

func getTiles(panoId string, zoom int, rlClient *RLHTTPClient) ([]tileData, error) {
	tiles := getTilesConfig(panoId, zoom)
	results := make([]tileData, 0, len(tiles))

	for _, tile := range tiles {
		fmt.Println("[Fetching]", tile.x, tile.y, "of", len(tiles))

		result, err := fetchTile(tile, rlClient)
		fmt.Println("[Done]", tile.x, tile.y, "of", len(tiles))
		if err != nil {
			return nil, err
		}

		td := tileData{
			x:    tile.x,
			y:    tile.y,
			data: result,
		}
		results = append(results, td)
	}

	return results, nil
}

func compositePano(tiles []tileData, zoom int) (*vips.ImageRef, error) {
	dim := getDimensionsFromZoom(zoom)

	pano, err := vips.Black(tileSize*dim.width, tileSize*dim.height)
	if err != nil {
		return nil, err
	}

	for _, tile := range tiles {
		image, err := vips.NewImageFromBuffer(tile.data)
		if err != nil {
			return nil, err
		}

		err = pano.Insert(image, tileSize*tile.x, tileSize*tile.y, false, &vips.ColorRGBA{})
		if err != nil {
			return nil, err
		}
	}

	left, top, width, height, err := pano.FindTrim(trimThreshold, &vips.Color{})
	if err != nil {
		return nil, err
	}

	err = pano.ExtractArea(left, top, width, height)
	if err != nil {
		return nil, err
	}

	return pano, nil
}

func main() {
	vips.Startup(nil)
	defer vips.Shutdown()

	panoId := "KGt-9AaQ7UTn_PgwRqtTOg"
	rl := rate.NewLimiter(rate.Every(200*time.Millisecond), 1)
	rlClient := NewClient(rl)

	tiles, err := getTiles(panoId, defaultZoom, rlClient)
	if err != nil {
		panic(err)
	}

	pano, err := compositePano(tiles, defaultZoom)
	if err != nil {
		panic(err)
	}

	ep := vips.NewDefaultJPEGExportParams()
	imageBytes, _, err := pano.Export(ep)
	if err != nil {
		panic(err)
	}

	err = os.WriteFile("pano.jpg", imageBytes, 0644)
}
