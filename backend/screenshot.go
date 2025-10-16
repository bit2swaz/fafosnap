package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"log"
	"math"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// takeScreenshot captures a full-page PNG from the provided URL after
// aggressively waking lazy-loaded content via simulated human scrolling.
func takeScreenshot(url string, timeoutSeconds int) ([]byte, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 90
	}

	log.Printf("capture start for %s (timeout %ds)", url, timeoutSeconds)

	masterCtx, cancelMaster := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancelMaster()

	allocatorOpts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(masterCtx, allocatorOpts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	tasks := chromedp.Tasks{
		chromedp.EmulateViewport(1920, 1080),
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
	}

	if err := chromedp.Run(browserCtx, tasks...); err != nil {
		return nil, fmt.Errorf("initial navigation for %s: %w", url, err)
	}

	settleDelay := 2 * time.Second

	if err := chromedp.Run(browserCtx, chromedp.Evaluate(`window.scrollTo(0, 0)`, nil)); err != nil {
		return nil, fmt.Errorf("prepare viewport for %s: %w", url, err)
	}
	if err := waitWithContext(browserCtx, settleDelay); err != nil {
		return nil, fmt.Errorf("initial settle for %s: %w", url, err)
	}

	log.Printf("section capture start for %s", url)

	buf, err := captureSections(browserCtx, settleDelay)
	if err != nil {
		return nil, err
	}

	log.Printf("capture complete for %s (%d bytes)", url, len(buf))

	return buf, nil
}

type scrollSnapshot struct {
	ScrollY      float64 `json:"scrollY"`
	ScrollHeight float64 `json:"scrollHeight"`
	ScrollWidth  float64 `json:"scrollWidth"`
	InnerHeight  float64 `json:"innerHeight"`
	InnerWidth   float64 `json:"innerWidth"`
	DeviceScale  float64 `json:"dpr"`
}

func captureSections(ctx context.Context, settle time.Duration) ([]byte, error) {
	const maxSections = 500

	type chunk struct {
		top int
		img *image.NRGBA
	}

	chunks := make([]chunk, 0, 12)
	var (
		totalHeight int
		maxWidth    int
		prevScroll  = -1.0
	)

	for section := 0; section < maxSections; section++ {
		state, err := fetchScrollSnapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("read scroll metrics: %w", err)
		}

		if section == 0 && state.ScrollHeight == 0 {
			return nil, fmt.Errorf("page reports zero scroll height")
		}

		if prevScroll >= 0 && math.Abs(state.ScrollY-prevScroll) < 0.5 {
			log.Printf("no further scroll progress detected at y=%.2f", state.ScrollY)
			if state.ScrollY+state.InnerHeight >= state.ScrollHeight-1 {
				break
			}
		}
		prevScroll = state.ScrollY

		captureHeight := state.InnerHeight
		if captureHeight <= 0 {
			captureHeight = 1080
		}
		maxCapture := state.ScrollHeight - state.ScrollY
		if captureHeight > maxCapture && maxCapture > 0 {
			captureHeight = maxCapture
		}
		if captureHeight < 1 {
			break
		}

		log.Printf("section %d: scrollY=%.0f height=%.0f/%0.f", section+1, state.ScrollY, captureHeight, state.ScrollHeight)

		chunkData, err := captureViewportChunk(ctx, state.ScrollY, state.ScrollWidth, captureHeight, math.Max(state.DeviceScale, 1.0))
		if err != nil {
			return nil, fmt.Errorf("capture section %d: %w", section+1, err)
		}

		img, err := png.Decode(bytes.NewReader(chunkData))
		if err != nil {
			return nil, fmt.Errorf("decode section %d: %w", section+1, err)
		}

		normalized := toNRGBA(img)
		chunks = append(chunks, chunk{top: totalHeight, img: normalized})
		totalHeight += normalized.Bounds().Dy()
		if w := normalized.Bounds().Dx(); w > maxWidth {
			maxWidth = w
		}

		remaining := state.ScrollHeight - (state.ScrollY + captureHeight)
		if remaining <= 1 {
			break
		}

		nextScroll := state.ScrollY + captureHeight
		if nextScroll > state.ScrollHeight {
			nextScroll = state.ScrollHeight
		}

		script := fmt.Sprintf("window.scrollTo(0, %f)", nextScroll)
		if err := chromedp.Run(ctx, chromedp.Evaluate(script, nil)); err != nil {
			return nil, fmt.Errorf("scroll to %.2f: %w", nextScroll, err)
		}

		if err := waitWithContext(ctx, settle); err != nil {
			return nil, fmt.Errorf("wait after scroll: %w", err)
		}
	}

	if len(chunks) == 0 {
		return nil, fmt.Errorf("no screenshot sections captured")
	}

	stitched := image.NewNRGBA(image.Rect(0, 0, maxWidth, totalHeight))
	for _, c := range chunks {
		dest := image.Rect(0, c.top, c.img.Bounds().Dx(), c.top+c.img.Bounds().Dy())
		draw.Draw(stitched, dest, c.img, image.Point{}, draw.Src)
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, stitched); err != nil {
		return nil, fmt.Errorf("encode stitched screenshot: %w", err)
	}

	return buffer.Bytes(), nil
}

func fetchScrollSnapshot(ctx context.Context) (scrollSnapshot, error) {
	var snapshot scrollSnapshot
	script := `({
		scrollY: window.scrollY || document.documentElement.scrollTop || 0,
		scrollHeight: Math.max(document.body.scrollHeight, document.documentElement.scrollHeight),
		scrollWidth: Math.max(document.body.scrollWidth, document.documentElement.scrollWidth),
		innerHeight: window.innerHeight,
		innerWidth: window.innerWidth,
		dpr: window.devicePixelRatio || 1
	})`
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &snapshot, chromedp.EvalAsValue)); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func captureViewportChunk(ctx context.Context, top, width, height, scale float64) ([]byte, error) {
	if height <= 0 {
		return nil, fmt.Errorf("invalid capture height %.2f", height)
	}
	if width <= 0 {
		width = 1920
	}

	var data []byte
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		clip := &page.Viewport{
			X:      0,
			Y:      top,
			Width:  width,
			Height: height,
			Scale:  scale,
		}

		result, err := page.CaptureScreenshot().
			WithFormat(page.CaptureScreenshotFormatPng).
			WithClip(clip).
			WithFromSurface(true).
			Do(ctx)
		if err != nil {
			return err
		}
		data = result
		return nil
	}))

	return data, err
}

func toNRGBA(img image.Image) *image.NRGBA {
	if existing, ok := img.(*image.NRGBA); ok && existing.Bounds().Min == (image.Point{}) {
		return existing
	}

	bounds := img.Bounds()
	normalized := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(normalized, normalized.Bounds(), img, bounds.Min, draw.Src)
	return normalized
}

func waitWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
